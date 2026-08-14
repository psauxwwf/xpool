package xpool

import (
	"context"

	appconfig "xpool/internal/config"
	"xpool/internal/xpool/runner"
	"xpool/internal/xpool/source"
)

const (
	DefaultInputPath         = appconfig.DefaultInputPath
	DefaultConfigPath        = appconfig.DefaultGeneratedPath
	DefaultXrayPath          = appconfig.DefaultXrayPath
	DefaultAPIAddress        = appconfig.DefaultAPIAddress
	DefaultStatusAddress     = appconfig.DefaultStatusAddress
	DefaultRotationInterval  = appconfig.DefaultRotationInterval
	DefaultCheckURL          = appconfig.DefaultCheckURL
	DefaultCheckInterval     = appconfig.DefaultCheckInterval
	DefaultReadyTTL          = appconfig.DefaultReadyTTL
	DefaultStartupTimeout    = appconfig.DefaultStartupTimeout
	DefaultCheckTimeout      = appconfig.DefaultCheckTimeout
	DefaultCheckConcurrency  = appconfig.DefaultCheckConcurrency
	DefaultCheckJitter       = appconfig.DefaultCheckJitter
	DefaultCheckMaxBytes     = appconfig.DefaultCheckMaxBytes
	DefaultReadySuccesses    = appconfig.DefaultReadySuccesses
	DefaultFailoverCooldown  = appconfig.DefaultFailoverCooldown
	DefaultPingTimeout       = appconfig.DefaultPingTimeout
	DefaultSampling          = appconfig.DefaultSampling
	DefaultGeneratedLogLevel = appconfig.DefaultGeneratedLogLevel
)

type Options = runner.Options

func Run(ctx context.Context, options Options) error {
	return runner.Run(ctx, options)
}

func RunConfig(ctx context.Context, config appconfig.Config) error {
	return runner.RunWithSource(ctx, runner.Options{
		InputPath:         config.Source.File,
		ConfigPath:        config.Xray.GeneratedPath,
		XrayPath:          config.Xray.BinaryPath,
		APIAddress:        config.Xray.APIAddress,
		StatusAddress:     config.Status.Address,
		RotationInterval:  config.Runtime.RotationInterval.Duration(),
		CheckURLs:         config.Health.CheckURLs,
		CheckInterval:     config.Health.CheckInterval.Duration(),
		ReadyTTL:          config.Health.ReadyTTL.Duration(),
		StartupTimeout:    config.Runtime.StartupTimeout.Duration(),
		CheckTimeout:      config.Health.Timeout.Duration(),
		CheckConcurrency:  config.Health.Concurrency,
		CheckJitter:       config.Health.Jitter.Duration(),
		CheckMaxBytes:     config.Health.MaxBytes,
		ReadySuccesses:    config.Health.ReadySuccesses,
		FailoverCooldown:  config.Runtime.FailoverCooldown.Duration(),
		PingTimeout:       config.Xray.PingTimeout.Duration(),
		Sampling:          config.Xray.Sampling,
		GeneratedLogLevel: config.Xray.GeneratedLogLevel,
	}, source.NewFile(config.Source.File))
}
