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
	s.recordFailure("track-1", "yt-dlp exited 1")

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
	if len(got.RecentFails) != 1 || got.RecentFails[0].TrackID != "track-1" {
		t.Errorf("recent fails = %+v, want one for track-1", got.RecentFails)
	}
}

func TestAcquisitionStatus_RecentFailsBounded(t *testing.T) {
	s := newStatusTestScheduler()
	for i := 0; i < recentFailCap+10; i++ {
		s.recordFailure("t", "boom")
	}
	if got := len(s.Status().RecentFails); got != recentFailCap {
		t.Errorf("recent fails = %d, want capped at %d", got, recentFailCap)
	}
}

func TestAcquisitionStatus_SnapshotIsACopy(t *testing.T) {
	s := newStatusTestScheduler()
	s.recordFailure("t", "boom")
	snap := s.Status()
	snap.RecentFails[0].Reason = "mutated"
	// Recording more must not be affected by the caller mutating the snapshot.
	if s.Status().RecentFails[0].Reason != "boom" {
		t.Error("Status() returned a shared slice; callers can corrupt internal state")
	}
}
