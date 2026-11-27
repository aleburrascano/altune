package leader

import "context"

func AdaptLocks[S lockSession](acquire func(context.Context) (S, error)) LockStore {
	return lockSource[S](acquire)
}

type lockSource[S lockSession] func(context.Context) (S, error)

func (a lockSource[S]) Acquire(ctx context.Context) (lockSession, error) {
	session, err := a(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}
