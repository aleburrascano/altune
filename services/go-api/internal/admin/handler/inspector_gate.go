package handler

import (
	"altune/go-api/internal/auth"
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	maxConcurrentReplays = 2
	replayInterval       = 2 * time.Second
	replayBurst          = 5
	busyRetryAfter       = 5 * time.Second
)

type inspectorGate struct {
	inFlight chan struct{}

	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

func newInspectorGate() *inspectorGate {
	return &inspectorGate{
		inFlight: make(chan struct{}, maxConcurrentReplays),
		buckets:  make(map[string]*rate.Limiter),
	}
}

func (g *inspectorGate) admit(ctx context.Context) (func(), *codedError) {
	if wait, admitted := g.spendToken(ctx); !admitted {
		return nil, replayThrottled(wait)
	}
	select {
	case g.inFlight <- struct{}{}:
		return func() { <-g.inFlight }, nil
	default:
		return nil, errReplaySlotsBusy
	}
}

func (g *inspectorGate) spendToken(ctx context.Context) (time.Duration, bool) {
	principal, authenticated := auth.UserIDFromContext(ctx)
	if !authenticated {
		return 0, true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	bucket := g.bucket(principal.String())
	if bucket.Allow() {
		return 0, true
	}
	reservation := bucket.Reserve()
	defer reservation.Cancel()
	return reservation.Delay(), false
}

func (g *inspectorGate) bucket(principal string) *rate.Limiter {
	if existing, ok := g.buckets[principal]; ok {
		return existing
	}
	created := rate.NewLimiter(rate.Every(replayInterval), replayBurst)
	g.buckets[principal] = created
	return created
}
