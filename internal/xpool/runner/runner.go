package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"xpool/internal/xpool/configgen"
	"xpool/internal/xpool/source"
	"xpool/pkg/health"
	"xpool/pkg/xray"
)

const (
	DefaultXrayPath         = "xray"
	DefaultConfigPath       = configgen.DefaultOutputPath
	DefaultStatusAddress    = "127.0.0.1:18080"
	DefaultRotationInterval = time.Minute
	DefaultReadyTTL         = 10 * time.Minute
	DefaultStartupTimeout   = 30 * time.Second
	DefaultCheckTimeout     = 3 * time.Second
)

type Options struct {
	InputPath         string
	ConfigPath        string
	XrayPath          string
	APIAddress        string
	StatusAddress     string
	RotationInterval  time.Duration
	CheckURLs         []string
	CheckInterval     time.Duration
	ReadyTTL          time.Duration
	StartupTimeout    time.Duration
	CheckTimeout      time.Duration
	PingTimeout       time.Duration
	Sampling          int
	GeneratedLogLevel string
}

type SourceStatus struct {
	Name       string `json:"name"`
	LoadedAt   string `json:"loaded_at"`
	ProxyCount int    `json:"proxy_count"`
}

type Status struct {
	Healthy          bool            `json:"healthy"`
	Serving          bool            `json:"serving"`
	StartedAt        string          `json:"started_at"`
	Current          string          `json:"current,omitempty"`
	BalancerTag      string          `json:"balancer_tag"`
	RotationInterval string          `json:"rotation_interval"`
	Source           SourceStatus    `json:"source"`
	Pool             health.Snapshot `json:"pool"`
	Switches         SwitchStatus    `json:"switches"`
}

type SwitchStatus struct {
	Rotations       int64  `json:"rotations"`
	Failovers       int64  `json:"failovers"`
	Failures        int64  `json:"failures"`
	LastReason      string `json:"last_reason,omitempty"`
	LastSelectedAt  any    `json:"last_selected_at"`
	LastError       string `json:"last_error,omitempty"`
	LastErrorReason string `json:"last_error_reason,omitempty"`
}

func ParseDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, value, err)
	}
	return duration, nil
}

func Run(ctx context.Context, options Options) error {
	options = withDefaults(options)
	return RunWithSource(ctx, options, source.File{Path: options.InputPath})
}

func RunWithSource(ctx context.Context, options Options, proxySource source.Source) error {
	options = withDefaults(options)
	if proxySource == nil {
		proxySource = source.File{Path: options.InputPath}
	}

	sourceResult, err := proxySource.Load(ctx)
	if err != nil {
		return err
	}

	result, err := configgen.GenerateFromProxies(sourceResult.Proxies, configgen.Options{
		OutputPath:    options.ConfigPath,
		APIAddress:    options.APIAddress,
		CheckURLs:     options.CheckURLs,
		CheckInterval: options.CheckInterval,
		PingTimeout:   options.PingTimeout,
		Sampling:      options.Sampling,
		LogLevel:      options.GeneratedLogLevel,
	})
	if err != nil {
		return err
	}
	slog.Info("generated Xray config", "path", result.OutputPath, "source", sourceResult.Name, "proxies", result.ProxyCount)

	process, err := xray.Start(options.XrayPath, options.ConfigPath)
	if err != nil {
		return err
	}
	defer process.Stop()

	if err := xray.WaitForAPI(ctx, options.XrayPath, options.APIAddress, configgen.DefaultBalancerTag, options.StartupTimeout); err != nil {
		return fmt.Errorf("wait for Xray API: %w", err)
	}
	slog.Info("Xray API is ready", "api", options.APIAddress)

	pool, err := health.NewPool(result.CheckRoutes, options.CheckURLs, options.CheckInterval, options.CheckTimeout, options.ReadyTTL)
	if err != nil {
		return err
	}
	pool.Start(ctx)

	controller := &Controller{
		xrayPath:         options.XrayPath,
		apiAddress:       options.APIAddress,
		balancerTag:      configgen.DefaultBalancerTag,
		rotationInterval: options.RotationInterval,
		startupTimeout:   options.StartupTimeout,
		pool:             pool,
		startedAt:        time.Now(),
		source: SourceStatus{
			Name:       sourceResult.Name,
			LoadedAt:   sourceResult.LoadedAt.UTC().Format(time.RFC3339),
			ProxyCount: len(sourceResult.Proxies),
		},
	}
	statusServer := startStatusServer(ctx, options.StatusAddress, controller)
	defer shutdownStatusServer(statusServer)

	return controller.Run(ctx, process.Done())
}

type Controller struct {
	xrayPath         string
	apiAddress       string
	balancerTag      string
	rotationInterval time.Duration
	startupTimeout   time.Duration
	pool             *health.Pool
	current          string
	serving          bool
	startedAt        time.Time
	source           SourceStatus
	rotations        int64
	failovers        int64
	switchFailures   int64
	lastReason       string
	lastSelectedAt   time.Time
	lastError        string
	lastErrorReason  string
	mu               sync.RWMutex
}

func (c *Controller) Run(ctx context.Context, xrayDone <-chan error) error {
	if err := c.waitReady(ctx, xrayDone); err != nil {
		return err
	}
	if err := c.switchToNext(ctx, "startup"); err != nil {
		c.recordSwitchFailure("startup", err)
		return err
	}

	rotationTicker := time.NewTicker(c.rotationInterval)
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
			if err := c.switchToNext(ctx, "failover"); err != nil {
				c.recordSwitchFailure("failover", err)
				slog.Error("failover switch failed", "error", err)
			}
		}
	}
}

func (c *Controller) waitReady(ctx context.Context, xrayDone <-chan error) error {
	ctx, cancel := context.WithTimeout(ctx, c.startupTimeout)
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
			return fmt.Errorf("ready pool is empty after %s", c.startupTimeout)
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
	if err := xray.OverrideBalancer(ctx, c.xrayPath, c.apiAddress, c.balancerTag, next); err != nil {
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
	return Status{
		Healthy:          pool.Ready > 0,
		Serving:          c.serving && currentReady,
		StartedAt:        c.startedAt.UTC().Format(time.RFC3339),
		Current:          c.current,
		BalancerTag:      c.balancerTag,
		RotationInterval: c.rotationInterval.String(),
		Source:           c.source,
		Pool:             pool,
		Switches: SwitchStatus{
			Rotations:       c.rotations,
			Failovers:       c.failovers,
			Failures:        c.switchFailures,
			LastReason:      c.lastReason,
			LastSelectedAt:  lastSelectedAt,
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
	}
	if reason == "failover" {
		c.failovers++
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

func startStatusServer(ctx context.Context, address string, controller *Controller) *http.Server {
	if address == "" || address == "off" {
		return nil
	}

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

	server := &http.Server{Addr: address, Handler: mux}
	go func() {
		slog.Info("status API listening", "addr", address)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("status API failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownStatusServer(server)
	}()
	return server
}

func shutdownStatusServer(server *http.Server) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func withDefaults(options Options) Options {
	if options.InputPath == "" {
		options.InputPath = configgen.DefaultInputPath
	}
	if options.ConfigPath == "" {
		options.ConfigPath = DefaultConfigPath
	}
	if options.XrayPath == "" {
		options.XrayPath = DefaultXrayPath
	}
	if options.APIAddress == "" {
		options.APIAddress = configgen.DefaultAPIAddress
	}
	if options.StatusAddress == "" {
		options.StatusAddress = DefaultStatusAddress
	}
	if options.RotationInterval == 0 {
		options.RotationInterval = DefaultRotationInterval
	}
	if len(options.CheckURLs) == 0 {
		options.CheckURLs = []string{configgen.DefaultCheckURL}
	}
	if options.CheckInterval == 0 {
		options.CheckInterval = configgen.DefaultCheckInterval
	}
	if options.ReadyTTL == 0 {
		options.ReadyTTL = DefaultReadyTTL
	}
	if options.StartupTimeout == 0 {
		options.StartupTimeout = DefaultStartupTimeout
	}
	if options.CheckTimeout == 0 {
		options.CheckTimeout = DefaultCheckTimeout
	}
	if options.PingTimeout == 0 {
		options.PingTimeout = configgen.DefaultPingTimeout
	}
	if options.Sampling == 0 {
		options.Sampling = configgen.DefaultSampling
	}
	if options.GeneratedLogLevel == "" {
		options.GeneratedLogLevel = "warning"
	}
	return options
}
