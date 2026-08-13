package xpool

import (
	"context"
	"time"

	"xpool/internal/xpool/configgen"
	"xpool/internal/xpool/runner"
)

const (
	DefaultInputPath         = configgen.DefaultInputPath
	DefaultConfigPath        = runner.DefaultConfigPath
	DefaultXrayPath          = runner.DefaultXrayPath
	DefaultAPIAddress        = configgen.DefaultAPIAddress
	DefaultRotationInterval  = runner.DefaultRotationInterval
	DefaultCheckURL          = configgen.DefaultCheckURL
	DefaultCheckInterval     = configgen.DefaultCheckInterval
	DefaultReadyTTL          = runner.DefaultReadyTTL
	DefaultStartupTimeout    = runner.DefaultStartupTimeout
	DefaultCheckTimeout      = runner.DefaultCheckTimeout
	DefaultPingTimeout       = configgen.DefaultPingTimeout
	DefaultSampling          = configgen.DefaultSampling
	DefaultGeneratedLogLevel = "warning"
)

type Options = runner.Options

func Run(ctx context.Context, options Options) error {
	return runner.Run(ctx, options)
}

func ParseURLs(raw string) ([]string, error) {
	return configgen.ParseURLs(raw)
}

func ParseDuration(name, value string) (time.Duration, error) {
	return runner.ParseDuration(name, value)
}
