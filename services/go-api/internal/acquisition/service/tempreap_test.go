package service

import (
	"testing"
	"time"
)

type recordingWorkspace struct {
	cutoffs []time.Time
}

func (r *recordingWorkspace) Create(string) (string, error) { return "", nil }
func (r *recordingWorkspace) Remove(string) error           { return nil }
func (r *recordingWorkspace) SweepAbandoned(abandonedBefore time.Time) int {
	r.cutoffs = append(r.cutoffs, abandonedBefore)
	return 0
}

func TestSweepStaleTempDirsSweepsOnceWithLiveJobCutoff(t *testing.T) {
	fake := &recordingWorkspace{}

	before := time.Now()
	SweepStaleTempDirs(fake)
	after := time.Now()

	if len(fake.cutoffs) != 1 {
		t.Fatalf("SweepAbandoned calls = %d, want 1", len(fake.cutoffs))
	}
	cutoff := fake.cutoffs[0]
	if cutoff.Before(before.Add(-maxLiveTempAge)) || cutoff.After(after.Add(-maxLiveTempAge)) {
		t.Errorf("cutoff = %v, want within [%v, %v]", cutoff, before.Add(-maxLiveTempAge), after.Add(-maxLiveTempAge))
	}
}

func TestSweepStaleTempDirsCutoffSparesJobsStillInsideAcquireTimeout(t *testing.T) {
	fake := &recordingWorkspace{}

	SweepStaleTempDirs(fake)

	if len(fake.cutoffs) != 1 {
		t.Fatalf("SweepAbandoned calls = %d, want 1", len(fake.cutoffs))
	}
	if age := time.Since(fake.cutoffs[0]); age < acquireTimeout {
		t.Errorf("cutoff is %v in the past, want at least acquireTimeout %v", age, acquireTimeout)
	}
}

func TestSweepStaleTempDirsRepeatedCallsNeverMoveCutoffBack(t *testing.T) {
	fake := &recordingWorkspace{}

	SweepStaleTempDirs(fake)
	SweepStaleTempDirs(fake)

	if len(fake.cutoffs) != 2 {
		t.Fatalf("SweepAbandoned calls = %d, want 2", len(fake.cutoffs))
	}
	if fake.cutoffs[1].Before(fake.cutoffs[0]) {
		t.Errorf("second cutoff %v earlier than first %v", fake.cutoffs[1], fake.cutoffs[0])
	}
}
