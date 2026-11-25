package security

import (
	"altune/overseer/internal/panicguard"
	"context"
	"time"
)

type scheduler struct {
	client   prober
	checks   []check
	interval time.Duration
	now      func() time.Time
	sink     func(suiteResult)
}

func newScheduler(client prober, checks []check, interval time.Duration, now func() time.Time, sink func(suiteResult)) *scheduler {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &scheduler{client: client, checks: checks, interval: interval, now: now, sink: sink}
}

func (s *scheduler) run(ctx context.Context) {
	s.safeRunOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.safeRunOnce(ctx)
		}
	}
}

func (s *scheduler) safeRunOnce(ctx context.Context) {
	panicguard.Contain(ctx, "security: suite run panicked", func() {
		s.sink(runSuite(ctx, s.client, s.checks, s.now))
	})
}
