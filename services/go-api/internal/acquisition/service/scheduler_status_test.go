package service

import (
	"sync"
	"testing"
)

// newStatusTestScheduler builds a scheduler suitable for exercising the
// in-memory status surface directly (no goroutines / no AcquireTrackAudioService).
func newStatusTestScheduler() *BackgroundAcquisitionScheduler {
	var wg sync.WaitGroup
	return NewBackgroundAcquisitionScheduler(nil, &wg, make(chan struct{}, 1))
}

func TestAcquisitionStatus_Counts(t *testing.T) {
	s := newStatusTestScheduler()

	s.inflightCount.Add(2)
	s.succeeded.Add(5)
	s.failed.Add(1)
	s.log.complete("track-1", "failed", "yt-dlp exited 1")

	got := s.Status()
	if got.InFlight != 2 {
		t.Errorf("in_flight = %d, want 2", got.InFlight)
	}
	if got.Succeeded != 5 {
		t.Errorf("succeeded = %d, want 5", got.Succeeded)
	}
	if got.Failed != 1 {
		t.Errorf("failed = %d, want 1", got.Failed)
	}
	if len(got.Recent) != 1 || got.Recent[0].TrackID != "track-1" ||
		got.Recent[0].State != "failed" || got.Recent[0].Reason != "yt-dlp exited 1" {
		t.Errorf("recent = %+v, want one failed entry for track-1", got.Recent)
	}
}

func TestAcquisitionStatus_RecentBounded(t *testing.T) {
	s := newStatusTestScheduler()
	// Failed jobs ride on the recent-terminal ring, so they are bounded by that
	// ring's cap rather than a separate failure cap.
	for i := 0; i < recentJobCap+10; i++ {
		s.log.complete("t", "failed", "boom")
	}
	if got := len(s.Status().Recent); got != recentJobCap {
		t.Errorf("recent = %d, want capped at %d", got, recentJobCap)
	}
}

func TestAcquisitionStatus_SnapshotIsACopy(t *testing.T) {
	s := newStatusTestScheduler()
	s.log.complete("t", "failed", "boom")
	snap := s.Status()
	snap.Recent[0].Reason = "mutated"
	// Recording more must not be affected by the caller mutating the snapshot.
	if s.Status().Recent[0].Reason != "boom" {
		t.Error("Status() returned a shared slice; callers can corrupt internal state")
	}
}
