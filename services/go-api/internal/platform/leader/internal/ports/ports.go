package ports

import "context"

type LockStore interface {
	Acquire(ctx context.Context) (LockSession, error)
}

type LockSession interface {
	TryAdvisoryLock(ctx context.Context, key int64) (bool, error)
	Ping(ctx context.Context) error
	AdvisoryUnlock(ctx context.Context, key int64) error
	Release()
}
