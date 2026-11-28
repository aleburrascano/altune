package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"time"
)

type claimer struct {
	gate          *admission
	queue         ports.DownloadQueue
	runner        *jobRunner
	jobs          background.Tracker
	wake          chan struct{}
	lanes         laneWakes
	timer         clock.TimerFunc
	pollInterval  time.Duration
	leaseDuration time.Duration
}

func (c *claimer) runWorker(ctx context.Context, claim func(context.Context) (<-chan struct{}, bool), wake chan struct{}) {
	for {
		if c.gate.closed.Load() {
			return
		}
		done, ok := c.nextJob(ctx, claim)
		if !ok && !c.idleWait(wake) {
			return
		}
		if !ok {
			continue
		}
		notifyWake(wake)
		<-done
	}
}

func (c *claimer) nextJob(ctx context.Context, claim func(context.Context) (<-chan struct{}, bool)) (<-chan struct{}, bool) {
	if c.gate.paused.Load() {
		return nil, false
	}
	return claim(ctx)
}

func (c *claimer) claimJob(ctx context.Context) (<-chan struct{}, bool) {
	return c.claimFrom(ctx, c.queue.Claim)
}

type claimFunc func(ctx context.Context, lease time.Duration) (ports.Job, error)

func (c *claimer) claimFrom(ctx context.Context, claim claimFunc) (<-chan struct{}, bool) {
	c.gate.mu.RLock()
	defer c.gate.mu.RUnlock()
	if c.gate.closed.Load() {
		return nil, false
	}
	job, ok := c.claimNext(ctx, claim)
	if !ok {
		return nil, false
	}
	done := make(chan struct{})
	c.jobs.Track(func() {
		defer close(done)
		c.runner.runClaimedJob(ctx, job)
	})
	return done, true
}

func (c *claimer) claimNext(ctx context.Context, claim claimFunc) (ports.Job, bool) {
	job, err := claim(ctx, c.leaseDuration)
	if errors.Is(err, domain.ErrNoJobAvailable) {
		return ports.Job{}, false
	}
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.claim_failed", logging.KeyError, err)
		return ports.Job{}, false
	}
	return job, true
}

type laneWakes struct {
	acquire chan struct{}
	upgrade chan struct{}
}

func newLaneWakes() laneWakes {
	return laneWakes{acquire: make(chan struct{}, 1), upgrade: make(chan struct{}, 1)}
}

func (c *claimer) fanOutWakes() {
	for {
		select {
		case <-c.wake:
			notifyWake(c.lanes.acquire)
			notifyWake(c.lanes.upgrade)
		case <-c.gate.stop:
			return
		}
	}
}

func notifyWake(wake chan<- struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (c *claimer) idleWait(wake <-chan struct{}) bool {
	fires, stop := c.timer(c.pollInterval)
	defer stop()
	select {
	case <-wake:
		return true
	case <-fires:
		return true
	case <-c.gate.stop:
		return false
	}
}
