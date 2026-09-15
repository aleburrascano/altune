package security

import (
	"context"
	"log/slog"
	"time"
)

// scheduler runs the security suite on its own low-frequency ticker, independent
// of the shell's collect cadence — active probing is deliberately infrequent. It
// is recover-guarded (a panicking probe never crashes the shell) and ctx-bound
// (it returns on cancellation, leaking no goroutine past the app's lifetime).
type scheduler struct {
	client   prober
	checks   []check
	interval time.Duration
	now      func() time.Time
	sink     func(suiteResult)
}

// newScheduler builds a scheduler. A non-positive interval is clamped to the
// default so the ticker can never be disabled or panic.
func newScheduler(client prober, checks []check, interval time.Duration, sink func(suiteResult)) *scheduler {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &scheduler{client: client, checks: checks, interval: interval, now: time.Now, sink: sink}
}

// run fires the suite once immediately (so the first render after startup has
// data), then on every tick until ctx is done. It is the single background
// goroutine the bucket owns and it returns on cancellation, so nothing leaks.
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

// safeRunOnce runs the suite with a recover, containing any panic so a single
// misbehaving probe can neither crash the process nor kill the scheduler: the
// ticker survives and the next run executes. run() is a long-lived background
// goroutine outside the shell's safeCollect recover, so containment lives here.
func (s *scheduler) safeRunOnce(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "security: suite run panicked", "recover", rec)
		}
	}()
	s.sink(runSuite(ctx, s.client, s.checks, s.now))
}
