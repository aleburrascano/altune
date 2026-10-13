package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"
)

type StuckScheduler struct {
	Deadline    time.Time
	HasDeadline bool
}

func (s *StuckScheduler) Schedule(ctx context.Context, _ shared.UserId, _ domain.TrackId, _ string) error {
	s.Deadline, s.HasDeadline = ctx.Deadline()
	<-ctx.Done()
	return ctx.Err()
}

func AssertScheduleDeadline(t *testing.T, sched *StuckScheduler, start time.Time, timeout time.Duration) {
	t.Helper()
	if !sched.HasDeadline {
		t.Fatal("Schedule received a context with no deadline")
	}
	latest := time.Now().Add(timeout)
	if !sched.Deadline.After(start) || sched.Deadline.After(latest) {
		t.Errorf("Schedule deadline = %v, want within (%v, %v]", sched.Deadline, start, latest)
	}
}
