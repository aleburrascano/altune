package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"
)

// stuckScheduler stands in for a Schedule call that never returns on its own:
// it records the deadline it was handed and blocks until its context ends.
type stuckScheduler struct {
	deadline    time.Time
	hasDeadline bool
}

func (s *stuckScheduler) Schedule(ctx context.Context, _ shared.UserId, _ domain.TrackId, _ string) error {
	s.deadline, s.hasDeadline = ctx.Deadline()
	<-ctx.Done()
	return ctx.Err()
}

// Regression for #1055: a request with no deadline of its own must still hand
// Schedule a bounded context, or a stuck call holds the handler goroutine forever.
func TestAddTrackService_ScheduleBoundedByTimeout(t *testing.T) {
	sched := &stuckScheduler{}
	svc := NewAddTrackService(catalogtest.NewTrackRepo(), WithAcquisitionScheduler(sched))

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	// Release the stuck call once its deadline has been observed so the test
	// does not wait the full production timeout.
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "T", Artist: "A", Album: "B"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertScheduleDeadline(t, sched, start)
}

func TestStreamTrackService_ReacquireBoundedByTimeout(t *testing.T) {
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	sched := &stuckScheduler{}
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	svc := NewStreamTrackService(repo, catalogtest.NewAudioStore(), WithStreamScheduler(sched))

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_ = svc.RecoverIfMissing(ctx, userId, track.ID)
	assertScheduleDeadline(t, sched, start)
}

// A caller that already carries a shorter budget keeps it, and a Schedule that
// runs out of it degrades the added track to failed rather than reporting success.
func TestAddTrackService_ScheduleTimeoutFailsTrack(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(&stuckScheduler{}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "T", Artist: "A", Album: "B"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed after a timed-out schedule", out.Track.AcquisitionStatus)
	}
	if out.Track.FailureReason == nil || *out.Track.FailureReason != string(domain.FailureAcquisitionRefused) {
		t.Errorf("failure reason = %v, want %q", out.Track.FailureReason, domain.FailureAcquisitionRefused)
	}
}

func assertScheduleDeadline(t *testing.T, sched *stuckScheduler, start time.Time) {
	t.Helper()
	if !sched.hasDeadline {
		t.Fatal("Schedule received a context with no deadline")
	}
	// The call happened between start and now, so its deadline must land in
	// (start, now+scheduleTimeout].
	latest := time.Now().Add(scheduleTimeout)
	if !sched.deadline.After(start) || sched.deadline.After(latest) {
		t.Errorf("Schedule deadline = %v, want within (%v, %v]", sched.deadline, start, latest)
	}
}
