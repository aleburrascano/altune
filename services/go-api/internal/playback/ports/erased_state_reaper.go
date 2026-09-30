package ports

import "context"

type ErasedStateReaper interface {
	ReapErasedStates(ctx context.Context) (int64, error)
}
