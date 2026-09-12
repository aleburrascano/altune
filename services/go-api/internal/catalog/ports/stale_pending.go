package ports

import (
	"context"
	"time"
)

// StalePendingFailer transitions pending tracks whose in-flight marker predates
// cutoff to failed. It is the durable-recovery counterpart to the in-memory
// acquisition scheduler: a track whose acquisition job was lost to a dead process
// stays pending forever otherwise, because streaming never recovers a non-ready
// track and the retry endpoint only admits failed ones.
type StalePendingFailer interface {
	FailStalePending(ctx context.Context, cutoff time.Time, reason string) (int, error)
}
