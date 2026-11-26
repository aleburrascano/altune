package memory

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"time"
)

type StuckScheduler struct {
	Deadline    time.Time
	HasDeadline bool
}

func (s *StuckScheduler) Schedule(ctx context.Context, _ ids.UserId, _ domain.TrackId, _ string) error {
	s.Deadline, s.HasDeadline = ctx.Deadline()
	<-ctx.Done()
	return fmt.Errorf("stuck scheduler released: %w", ctx.Err())
}

func AssertScheduleDeadline(t TB, sched *StuckScheduler, start time.Time, timeout time.Duration, now clock.Func) {
	t.Helper()
	if !sched.HasDeadline {
		t.Fatal("Schedule received a context with no deadline")
	}
	latest := now().Add(timeout)
	if !sched.Deadline.After(start) || sched.Deadline.After(latest) {
		t.Errorf("Schedule deadline = %v, want within (%v, %v]", sched.Deadline, start, latest)
	}
}
