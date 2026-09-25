package history

import (
	"altune/overseer/internal/core"
	"context"
	"log/slog"
	"time"
)

const (
	DefaultCap       = 20000
	DefaultRetention = 7 * 24 * time.Hour
	RawRetention     = 24 * time.Hour
)

type Store interface {
	core.Series
	Names(bucket string) ([]string, error)
	RunPruner(ctx context.Context)
	Close() error
}

func Open(path string, opts ...Option) Database {
	store, err := openDisk(path, opts...)
	if err != nil {
		slog.Error("history: unavailable", "path", path, "error", err)
		return unavailable{}
	}
	return store
}

type unavailable struct{}

func (unavailable) Record(string, string, core.Point) {}

func (unavailable) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func (unavailable) Names(string) ([]string, error) { return nil, nil }

func (unavailable) RunPruner(context.Context) {}

func (unavailable) Close() error { return nil }
