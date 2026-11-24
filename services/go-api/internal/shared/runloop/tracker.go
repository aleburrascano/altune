package runloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	backgroundTaskPanic   = "background.task_panic"
	backgroundTaskDropped = "background.task_dropped"
)

var ErrTrackerLimit = errors.New("runloop: a tracker needs MaxInFlight of at least 1")

type TrackerLimits struct {
	MaxInFlight int
}

type Tracker struct {
	wg       sync.WaitGroup
	mu       sync.Mutex
	draining bool
	slots    chan struct{}
	dropped  atomic.Uint64
}

func NewTracker(limits TrackerLimits) (*Tracker, error) {
	if limits.MaxInFlight < 1 {
		return nil, fmt.Errorf("%w, got %d", ErrTrackerLimit, limits.MaxInFlight)
	}
	return &Tracker{slots: make(chan struct{}, limits.MaxInFlight)}, nil
}

func (r *Tracker) Track(fn func()) {
	tracked := r.join()
	go func() {
		if tracked {
			defer r.wg.Done()
		}
		fn()
	}()
}

func (r *Tracker) Launch(parentCtx context.Context, label string, fn func(ctx context.Context)) {
	ctx := context.WithoutCancel(parentCtx)
	r.Track(func() {
		defer RecoverContext(ctx, "search.v2.background_panic", "label", label)
		fn(ctx)
	})
}

func (r *Tracker) LaunchWithin(parent context.Context, label string, budget time.Duration, fn func(ctx context.Context)) bool {
	if !r.admit() {
		r.dropped.Add(1)
		slog.WarnContext(parent, backgroundTaskDropped, "label", label)
		return false
	}
	go func() {
		defer r.release()
		ctx, cancel := Detach(parent, budget)
		defer cancel()
		defer RecoverContext(ctx, backgroundTaskPanic, "label", label)
		fn(ctx)
	}()
	return true
}

func (r *Tracker) Dropped() uint64 { return r.dropped.Load() }

func (r *Tracker) Drain(ctx context.Context) bool {
	r.mu.Lock()
	r.draining = true
	r.mu.Unlock()
	return WaitWithin(ctx, r.wg.Wait)
}

func (r *Tracker) Wait() { r.wg.Wait() }

func (r *Tracker) join() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.draining {
		return false
	}
	r.wg.Add(1)
	return true
}

func (r *Tracker) admit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.draining || !r.takeSlot() {
		return false
	}
	r.wg.Add(1)
	return true
}

func (r *Tracker) takeSlot() bool {
	if r.slots == nil {
		return true
	}
	select {
	case r.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *Tracker) release() {
	if r.slots != nil {
		<-r.slots
	}
	r.wg.Done()
}
