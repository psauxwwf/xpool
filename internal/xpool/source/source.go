package source

import (
	"context"
	"time"

	"xpool/internal/xpool/configgen"
)

type Result struct {
	Name          string
	Proxies       []configgen.Proxy
	LoadedAt      time.Time
	InvalidCount  int
	InvalidErrors []configgen.ParseError
}

type Source interface {
	Load(context.Context) (Result, error)
}

type File struct {
	Path string
}

func NewFile(path string) File {
	return File{Path: path}
}

func (s File) Load(ctx context.Context) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	proxies, invalid, err := configgen.ReadProxies(s.Path)
	if err != nil {
		return Result{}, err
	}
	invalidCount := len(invalid)
	if len(invalid) > 10 {
		invalid = invalid[:10]
	}
	return Result{
		Name:          s.Path,
		Proxies:       proxies,
		LoadedAt:      time.Now(),
		InvalidCount:  invalidCount,
		InvalidErrors: invalid,
	}, nil
}
