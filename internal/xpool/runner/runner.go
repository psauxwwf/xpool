package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	appconfig "xpool/internal/config"
	"xpool/internal/xpool/configgen"
	"xpool/internal/xpool/source"
	"xpool/pkg/health"
	"xpool/pkg/xray"
)

type SourceStatus struct {
	Name          string                 `json:"name"`
	LoadedAt      string                 `json:"loaded_at"`
	ProxyCount    int                    `json:"proxy_count"`
	InvalidCount  int                    `json:"invalid_count"`
	InvalidErrors []configgen.ParseError `json:"invalid_errors,omitempty"`
}

type Status struct {
	Healthy                 bool            `json:"healthy"`
	Serving                 bool            `json:"serving"`
	StartedAt               string          `json:"started_at"`
	Uptime                  string          `json:"uptime"`
	XrayAPIReady            bool            `json:"xray_api_ready"`
	XrayPID                 int             `json:"xray_pid"`
	Current                 string          `json:"current,omitempty"`
	BalancerTag             string          `json:"balancer_tag"`
	ProxyRotationInterval   string          `json:"proxy_rotation_interval"`
	FailoverAttemptCooldown string          `json:"failover_attempt_cooldown"`
	NextRotationAt          any             `json:"next_rotation_at"`
	Source                  SourceStatus    `json:"source"`
	Pool                    health.Snapshot `json:"pool"`
	Switches                SwitchStatus    `json:"switches"`
}

type SwitchStatus struct {
	Rotations       int64  `json:"rotations"`
	Failovers       int64  `json:"failovers"`
	Failures        int64  `json:"failures"`
	LastReason      string `json:"last_reason,omitempty"`
	LastSelectedAt  any    `json:"last_selected_at"`
	LastRotationAt  any    `json:"last_rotation_at"`
	LastFailoverAt  any    `json:"last_failover_at"`
	LastError       string `json:"last_error,omitempty"`
	LastErrorReason string `json:"last_error_reason,omitempty"`
}

func Run(ctx context.Context, config appconfig.Config) error {
	return RunWithSource(ctx, config, source.NewFile(config.Source.ProxyListFilePath))
}

func RunWithSource(ctx context.Context, config appconfig.Config, proxySource source.Source) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if proxySource == nil {
		proxySource = source.NewFile(config.Source.ProxyListFilePath)
	}

	sourceResult, err := proxySource.Load(ctx)
	if err != nil {
		return err
	}
	if len(sourceResult.Proxies) == 0 {
		return fmt.Errorf("no valid proxies found in %s (invalid: %d)", sourceResult.Name, sourceResult.InvalidCount)
	}

	generator := configgen.NewGenerator(config)
	result, err := generator.Generate(sourceResult.Proxies)
	if err != nil {
		return err
	}
	slog.Info("generated Xray config", "path", result.GeneratedConfigPath, "source", sourceResult.Name, "proxies", result.ProxyCount)
	xrayRuntime := xray.NewRuntime(config.Xray.ExecutablePath)
	if err := xrayRuntime.ValidateConfig(ctx, config.Xray.GeneratedConfigPath, config.Runtime.StartupReadyTimeout.Duration()); err != nil {
		return err
	}
	slog.Info("validated Xray config", "path", config.Xray.GeneratedConfigPath)

	process, err := xrayRuntime.Start(config.Xray.GeneratedConfigPath)
	if err != nil {
		return err
	}
	defer process.Stop()

	if err := xrayRuntime.WaitForAPI(ctx, config.Xray.GRPCAPIAddress, configgen.DefaultBalancerTag, config.Runtime.StartupReadyTimeout.Duration()); err != nil {
		return fmt.Errorf("wait for Xray API: %w", err)
	}
	slog.Info("Xray API is ready", "api", config.Xray.GRPCAPIAddress)

	pool, err := health.NewPool(result.CheckRoutes, config.Health.FullDownloadCheckURLs, health.Options{
		ActiveRoutesCheckInterval: config.Health.ActiveRoutesCheckInterval.Duration(),
		FullDownloadCheckTimeout:  config.Health.FullDownloadCheckTimeout.Duration(),
		SuccessfulCheckReadyTTL:   config.Health.SuccessfulCheckReadyTTL.Duration(),
		MaxConcurrentChecks:       config.Health.MaxConcurrentChecks,
		CheckStartJitter:          config.Health.CheckStartJitter.Duration(),
		MaxDownloadBytes:          config.Health.MaxDownloadBytes,
		RequiredSuccessfulChecks:  config.Health.RequiredSuccessfulChecks,
		FailedChecksBeforeRetire:  config.Health.FailedChecksBeforeRetire,
	})
	if err != nil {
		return err
	}
	controller := NewController(ControllerOptions{
		xrayRuntime:             xrayRuntime,
		apiAddress:              config.Xray.GRPCAPIAddress,
		balancerTag:             configgen.DefaultBalancerTag,
		proxyRotationInterval:   config.Runtime.ProxyRotationInterval.Duration(),
		failoverAttemptCooldown: config.Runtime.FailoverAttemptCooldown.Duration(),
		startupReadyTimeout:     config.Runtime.StartupReadyTimeout.Duration(),
		pool:                    pool,
		startedAt:               time.Now(),
		xrayAPIReady:            true,
		xrayPID:                 process.PID(),
		source: SourceStatus{
			Name:          sourceResult.Name,
			LoadedAt:      sourceResult.LoadedAt.UTC().Format(time.RFC3339),
			ProxyCount:    len(sourceResult.Proxies),
			InvalidCount:  sourceResult.InvalidCount,
			InvalidErrors: sourceResult.InvalidErrors,
		},
	})
	statusServer, err := startStatusServer(ctx, config.Status.ListenAddress, controller)
	if err != nil {
		return err
	}
	defer shutdownStatusServer(statusServer)

	pool.Start(ctx)

	return controller.Run(ctx, process.Done())
}

type Controller struct {
	xrayRuntime             xray.Runtime
	apiAddress              string
	balancerTag             string
	proxyRotationInterval   time.Duration
	failoverAttemptCooldown time.Duration
	startupReadyTimeout     time.Duration
	pool                    *health.Pool
	current                 string
	serving                 bool
	startedAt               time.Time
	xrayAPIReady            bool
	xrayPID                 int
	source                  SourceStatus
	rotations               int64
	failovers               int64
	switchFailures          int64
	lastReason              string
	lastSelectedAt          time.Time
	lastRotationAt          time.Time
	lastFailoverAt          time.Time
	lastError               string
	lastErrorReason         string
	mu                      sync.RWMutex
}

type ControllerOptions struct {
	xrayRuntime             xray.Runtime
	apiAddress              string
	balancerTag             string
	proxyRotationInterval   time.Duration
	failoverAttemptCooldown time.Duration
	startupReadyTimeout     time.Duration
	pool                    *health.Pool
	startedAt               time.Time
	xrayAPIReady            bool
	xrayPID                 int
	source                  SourceStatus
}

func NewController(options ControllerOptions) *Controller {
	return &Controller{
		xrayRuntime:             options.xrayRuntime,
		apiAddress:              options.apiAddress,
		balancerTag:             options.balancerTag,
		proxyRotationInterval:   options.proxyRotationInterval,
		failoverAttemptCooldown: options.failoverAttemptCooldown,
		startupReadyTimeout:     options.startupReadyTimeout,
		pool:                    options.pool,
		startedAt:               options.startedAt,
		xrayAPIReady:            options.xrayAPIReady,
		xrayPID:                 options.xrayPID,
		source:                  options.source,
	}
}

func (c *Controller) Run(ctx context.Context, xrayDone <-chan error) error {
	if err := c.waitReady(ctx, xrayDone); err != nil {
		return err
	}
	if err := c.switchToNext(ctx, "startup"); err != nil {
		c.recordSwitchFailure("startup", err)
		return err
	}

	rotationTicker := time.NewTicker(c.proxyRotationInterval)
	defer rotationTicker.Stop()
	failoverTicker := time.NewTicker(time.Second)
	defer failoverTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-xrayDone:
			if err != nil {
				return fmt.Errorf("Xray exited: %w", err)
			}
			return nil
		case <-rotationTicker.C:
			if err := c.switchToNext(ctx, "rotation"); err != nil {
				c.recordSwitchFailure("rotation", err)
				slog.Error("rotation switch failed", "error", err)
			}
		case <-failoverTicker.C:
			if c.currentTag() != "" && c.pool.IsReady(c.currentTag()) {
				continue
			}
			if c.inFailoverCooldown() {
				continue
			}
			if err := c.switchToNext(ctx, "failover"); err != nil {
				c.recordSwitchFailure("failover", err)
				slog.Error("failover switch failed", "error", err)
			}
		}
	}
}

func (c *Controller) waitReady(ctx context.Context, xrayDone <-chan error) error {
	ctx, cancel := context.WithTimeout(ctx, c.startupReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready := c.pool.Ready(); len(ready) > 0 {
			slog.Info("ready pool initialized", "count", len(ready), "first", ready[0].Tag)
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("ready pool is empty after %s", c.startupReadyTimeout)
		case err := <-xrayDone:
			if err != nil {
				return fmt.Errorf("Xray exited before ready pool initialized: %w", err)
			}
			return fmt.Errorf("Xray exited before ready pool initialized")
		case <-ticker.C:
		}
	}
}

func (c *Controller) switchToNext(ctx context.Context, reason string) error {
	ready := c.pool.Ready()
	if len(ready) == 0 {
		return fmt.Errorf("ready pool is empty")
	}

	next := ready[0].Tag
	current := c.currentTag()
	if len(ready) > 1 && current != "" {
		for i, state := range ready {
			if state.Tag == current {
				next = ready[(i+1)%len(ready)].Tag
				break
			}
		}
	}
	if next == current {
		slog.Debug("proxy override unchanged", "tag", next, "reason", reason)
		return nil
	}
	if !c.pool.IsReady(next) {
		return fmt.Errorf("candidate %s is no longer ready", next)
	}
	if err := c.xrayRuntime.OverrideBalancer(ctx, c.apiAddress, c.balancerTag, next); err != nil {
		return err
	}
	previous := current
	c.recordSwitchSuccess(reason, next)
	slog.Info("selected ready proxy", "previous", previous, "tag", next, "reason", reason, "ready", len(ready), "ready_tags", health.StateTags(ready))
	return nil
}

func (c *Controller) Healthy() bool {
	return c.pool.Snapshot().Ready > 0
}

func (c *Controller) Status() Status {
	pool := c.pool.Snapshot()
	c.mu.RLock()
	defer c.mu.RUnlock()
	currentReady := false
	for _, state := range pool.States {
		if state.Tag == c.current && state.Ready {
			currentReady = true
			break
		}
	}

	lastSelectedAt := any(nil)
	if !c.lastSelectedAt.IsZero() {
		lastSelectedAt = c.lastSelectedAt.UTC().Format(time.RFC3339)
	}
	lastRotationAt := any(nil)
	if !c.lastRotationAt.IsZero() {
		lastRotationAt = c.lastRotationAt.UTC().Format(time.RFC3339)
	}
	lastFailoverAt := any(nil)
	if !c.lastFailoverAt.IsZero() {
		lastFailoverAt = c.lastFailoverAt.UTC().Format(time.RFC3339)
	}
	nextRotationAt := any(nil)
	if !c.lastSelectedAt.IsZero() {
		nextRotationAt = c.lastSelectedAt.Add(c.proxyRotationInterval).UTC().Format(time.RFC3339)
	}
	return Status{
		Healthy:                 pool.Ready > 0,
		Serving:                 c.serving && currentReady,
		StartedAt:               c.startedAt.UTC().Format(time.RFC3339),
		Uptime:                  time.Since(c.startedAt).Round(time.Second).String(),
		XrayAPIReady:            c.xrayAPIReady,
		XrayPID:                 c.xrayPID,
		Current:                 c.current,
		BalancerTag:             c.balancerTag,
		ProxyRotationInterval:   c.proxyRotationInterval.String(),
		FailoverAttemptCooldown: c.failoverAttemptCooldown.String(),
		NextRotationAt:          nextRotationAt,
		Source:                  c.source,
		Pool:                    pool,
		Switches: SwitchStatus{
			Rotations:       c.rotations,
			Failovers:       c.failovers,
			Failures:        c.switchFailures,
			LastReason:      c.lastReason,
			LastSelectedAt:  lastSelectedAt,
			LastRotationAt:  lastRotationAt,
			LastFailoverAt:  lastFailoverAt,
			LastError:       c.lastError,
			LastErrorReason: c.lastErrorReason,
		},
	}
}

func (c *Controller) currentTag() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

func (c *Controller) recordSwitchSuccess(reason, tag string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = tag
	c.serving = true
	c.lastReason = reason
	c.lastSelectedAt = time.Now()
	c.lastError = ""
	c.lastErrorReason = ""
	if reason == "rotation" {
		c.rotations++
		c.lastRotationAt = c.lastSelectedAt
	}
	if reason == "failover" {
		c.failovers++
		c.lastFailoverAt = c.lastSelectedAt
	}
}

func (c *Controller) recordSwitchFailure(reason string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.switchFailures++
	c.lastError = err.Error()
	c.lastErrorReason = reason
	if reason == "startup" || reason == "failover" {
		c.serving = false
	}
}

func (c *Controller) inFailoverCooldown() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.failoverAttemptCooldown <= 0 || c.lastSelectedAt.IsZero() {
		return false
	}
	return time.Since(c.lastSelectedAt) < c.failoverAttemptCooldown
}

func startStatusServer(ctx context.Context, address string, controller *Controller) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		status := controller.Status()
		if !status.Healthy {
			http.Error(w, "ready pool is empty", http.StatusServiceUnavailable)
			return
		}
		if !status.Serving {
			http.Error(w, "not serving", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(controller.Status())
	})

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen status API on %s: %w", address, err)
	}
	server := &http.Server{Addr: address, Handler: mux}
	go func() {
		slog.Info("status API listening", "addr", address)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("status API failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownStatusServer(server)
	}()
	return server, nil
}

func shutdownStatusServer(server *http.Server) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
