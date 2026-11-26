package leader

import "context"

func AdaptLocks[S LockSession](acquire func(context.Context) (S, error)) LockStore {
	return lockSource[S](acquire)
}

type lockSource[S LockSession] func(context.Context) (S, error)

func (a lockSource[S]) Acquire(ctx context.Context) (LockSession, error) {
	session, err := a(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}
