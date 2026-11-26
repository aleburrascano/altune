package ports

import (
	"context"
	"time"
)

const TempDirPrefix = "altune-acquire-"

type ScratchFolder interface {
	Create(pattern string) (string, error)
	Remove(path string) error
	Size(path string) (int64, error)
	SweepAbandoned(ctx context.Context, abandonedBefore time.Time) int
}
