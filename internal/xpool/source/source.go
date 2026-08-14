package source

import (
	"context"
	"time"

	"xpool/internal/xpool/configgen"
)

type Result struct {
	Name     string
	Proxies  []configgen.Proxy
	LoadedAt time.Time
}

type Source interface {
	Load(context.Context) (Result, error)
}

type File struct {
	Path string
}

func (s File) Load(ctx context.Context) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	proxies, err := configgen.ReadProxies(s.Path)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Name:     s.Path,
		Proxies:  proxies,
		LoadedAt: time.Now(),
	}, nil
}
