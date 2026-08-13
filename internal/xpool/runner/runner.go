package runner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"xpool/internal/xpool/configgen"
	"xpool/pkg/health"
	"xpool/pkg/xray"
)

const (
	DefaultXrayPath         = "xray"
	DefaultConfigPath       = configgen.DefaultOutputPath
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

func ParseDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, value, err)
	}
	return duration, nil
}

func Run(ctx context.Context, options Options) error {
	options = withDefaults(options)

	result, err := configgen.Generate(configgen.Options{
		InputPath:     options.InputPath,
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
	slog.Info("generated Xray config", "path", result.OutputPath, "proxies", result.ProxyCount)

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

	controller := Controller{
		xrayPath:         options.XrayPath,
		apiAddress:       options.APIAddress,
		balancerTag:      configgen.DefaultBalancerTag,
		rotationInterval: options.RotationInterval,
		startupTimeout:   options.StartupTimeout,
		pool:             pool,
	}
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
}

func (c *Controller) Run(ctx context.Context, xrayDone <-chan error) error {
	if err := c.waitReady(ctx, xrayDone); err != nil {
		return err
	}
	if err := c.switchToNext(ctx, "startup"); err != nil {
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
				slog.Error("rotation switch failed", "error", err)
			}
		case <-failoverTicker.C:
			if c.current != "" && c.pool.IsReady(c.current) {
				continue
			}
			if err := c.switchToNext(ctx, "failover"); err != nil {
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
	if len(ready) > 1 && c.current != "" {
		for i, state := range ready {
			if state.Tag == c.current {
				next = ready[(i+1)%len(ready)].Tag
				break
			}
		}
	}
	if next == c.current {
		slog.Debug("proxy override unchanged", "tag", next, "reason", reason)
		return nil
	}
	if err := xray.OverrideBalancer(ctx, c.xrayPath, c.apiAddress, c.balancerTag, next); err != nil {
		return err
	}
	previous := c.current
	c.current = next
	slog.Info("selected ready proxy", "previous", previous, "tag", next, "reason", reason, "ready", len(ready), "ready_tags", health.StateTags(ready))
	return nil
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
