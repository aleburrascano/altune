package service

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrLimitNotConfigured = errors.New("download limit is not configured; build it with LimitedTo or Unlimited")
	ErrLimitBelowOne      = errors.New("download limit must allow at least one download")
)

type ConcurrencyLimit struct {
	slots     chan struct{}
	unlimited bool
}

func LimitedTo(n int) (*ConcurrencyLimit, error) {
	if n < 1 {
		return nil, fmt.Errorf("%w: got %d", ErrLimitBelowOne, n)
	}
	return &ConcurrencyLimit{slots: make(chan struct{}, n)}, nil
}

func MustLimitedTo(n int) *ConcurrencyLimit {
	limit, err := LimitedTo(n)
	if err != nil {
		panic(err)
	}
	return limit
}

func Unlimited() *ConcurrencyLimit { return &ConcurrencyLimit{unlimited: true} }

func (l *ConcurrencyLimit) Acquire(ctx context.Context) error {
	if l.unlimited {
		return nil
	}
	if l.slots == nil {
		return ErrLimitNotConfigured
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *ConcurrencyLimit) Release() {
	if l.unlimited || l.slots == nil {
		return
	}
	<-l.slots
}
