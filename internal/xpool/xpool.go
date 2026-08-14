package xpool

import (
	"context"

	"xpool/internal/config"
	"xpool/internal/xpool/runner"
)

type Config = config.Config

func Run(ctx context.Context, config Config) error {
	return runner.Run(ctx, config)
}
