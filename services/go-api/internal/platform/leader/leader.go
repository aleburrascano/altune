package leader

import (
	"altune/go-api/internal/platform/leader/internal/domain"
	"altune/go-api/internal/platform/leader/internal/ports"
	"altune/go-api/internal/platform/leader/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"time"
)

type (
	Counters  = domain.Counters
	Election  = service.Election
	LockStore = ports.LockStore
)

func New(locks LockStore, key int64, interval time.Duration, now clock.Func, newTimer clock.TimerFunc, newTicker clock.TickerFunc) (*Election, error) {
	election, err := service.NewElection(locks, key, interval, now, newTimer, newTicker)
	if err != nil {
		return nil, fmt.Errorf("leader: %w", err)
	}
	return election, nil
}

func AdaptLocks[S ports.LockSession](acquire func(context.Context) (S, error)) LockStore {
	return service.AdaptLocks(acquire)
}
