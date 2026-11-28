package service

import (
	"altune/go-api/internal/platform/leader/internal/ports"
	"context"
)

func AdaptLocks[S ports.LockSession](acquire func(context.Context) (S, error)) ports.LockStore {
	return lockSource[S](acquire)
}

type lockSource[S ports.LockSession] func(context.Context) (S, error)

func (a lockSource[S]) Acquire(ctx context.Context) (ports.LockSession, error) {
	session, err := a(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}
