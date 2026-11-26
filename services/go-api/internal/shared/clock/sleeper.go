package clock

import (
	"context"
	"time"
)

type Sleeper func(ctx context.Context, d time.Duration) error

func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type TimerFunc func(d time.Duration) (fires <-chan time.Time, stop func() bool)

func NewTimer(d time.Duration) (<-chan time.Time, func() bool) {
	timer := time.NewTimer(d)
	return timer.C, timer.Stop
}

type TickerFunc func(d time.Duration) (ticks <-chan time.Time, stop func())

func NewTicker(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}
