package history

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history/internal/adapters/sqlite"
	"context"
	"log/slog"
	"time"
)

type Store interface {
	core.Series
	Names(bucket string) ([]string, error)
	RunPruner(ctx context.Context)
	Close() error
}

type Signals interface {
	LoadSignals(ctx context.Context, bucket, ring string, limit int) ([]core.Signal, error)
	AppendSignals(bucket, ring string, capacity int, fresh []core.Signal)
}

type Database interface {
	Store
	Signals
}

var WithSchemaTimeout = sqlite.WithSchemaTimeout

func Open(path string, opts ...sqlite.Option) Database {
	store, err := sqlite.Open(path, opts...)
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

func (unavailable) LoadSignals(context.Context, string, string, int) ([]core.Signal, error) {
	return nil, nil
}

func (unavailable) AppendSignals(string, string, int, []core.Signal) {}
