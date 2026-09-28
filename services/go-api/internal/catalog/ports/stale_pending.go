package ports

import (
	"context"
	"time"
)

type StalePendingFailer interface {
	FailStalePending(ctx context.Context, cutoff time.Time, reason string) (int, error)
}
