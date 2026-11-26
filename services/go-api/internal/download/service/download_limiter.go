package service

import (
	"altune/go-api/internal/shared/parallel"
	"context"
	"errors"
	"fmt"
)

var (
	ErrLimitNotConfigured = errors.New("download limit is not configured; build it with LimitedTo or Unlimited")
	ErrLimitBelowOne      = errors.New("download limit must allow at least one download")
)

type limitMode int

const (
	limitUnset limitMode = iota
	limitCapped
	limitUncapped
)

type ConcurrencyLimit struct {
	slots *parallel.Slots
	mode  limitMode
}

func LimitedTo(n int) (*ConcurrencyLimit, error) {
	if n < 1 {
		return nil, fmt.Errorf("%w: got %d", ErrLimitBelowOne, n)
	}
	return &ConcurrencyLimit{slots: parallel.MustNewSlots(n), mode: limitCapped}, nil
}

func MustLimitedTo(n int) *ConcurrencyLimit {
	limit, err := LimitedTo(n)
	if err != nil {
		panic(err)
	}
	return limit
}

func Unlimited() *ConcurrencyLimit { return &ConcurrencyLimit{mode: limitUncapped} }

func (l *ConcurrencyLimit) Acquire(ctx context.Context) error {
	switch l.mode {
	case limitUncapped:
		return nil
	case limitCapped:
		return l.slots.Acquire(ctx)
	default:
		return ErrLimitNotConfigured
	}
}

func (l *ConcurrencyLimit) Release() {
	if l.mode != limitCapped {
		return
	}
	l.slots.Release()
}
