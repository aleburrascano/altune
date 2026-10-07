package service

import (
	"altune/go-api/internal/acquisition/ports"
	"os"
	"sync"
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

type osWorkspace struct{}

func (osWorkspace) Create(pattern string) (string, error) {
	return os.MkdirTemp("", ports.TempDirPrefix+pattern)
}

func (osWorkspace) Remove(path string) error { return os.RemoveAll(path) }

func (osWorkspace) SweepAbandoned(time.Time) int { return 0 }

func init() { defaultTempWorkspace = osWorkspace{} }

type callRecordingWorkspace struct {
	inner ports.TempWorkspace

	mu       sync.Mutex
	patterns []string
	created  []string
	removed  []string
}

func newCallRecordingWorkspace() *callRecordingWorkspace {
	return &callRecordingWorkspace{inner: osWorkspace{}}
}

func (r *callRecordingWorkspace) Create(pattern string) (string, error) {
	dir, err := r.inner.Create(pattern)
	if err == nil {
		r.mu.Lock()
		r.patterns = append(r.patterns, pattern)
		r.created = append(r.created, dir)
		r.mu.Unlock()
	}
	return dir, err
}

func (r *callRecordingWorkspace) Remove(path string) error {
	r.mu.Lock()
	r.removed = append(r.removed, path)
	r.mu.Unlock()
	return r.inner.Remove(path)
}

func (r *callRecordingWorkspace) SweepAbandoned(t time.Time) int { return r.inner.SweepAbandoned(t) }

func (r *callRecordingWorkspace) createdDirs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.created...)
}

func (r *callRecordingWorkspace) removedPaths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.removed...)
}

func (r *callRecordingWorkspace) removedCount(path string) int {
	n := 0
	for _, p := range r.removedPaths() {
		if p == path {
			n++
		}
	}
	return n
}

func (r *callRecordingWorkspace) liveDirs() []string {
	var live []string
	for _, dir := range r.createdDirs() {
		if r.removedCount(dir) == 0 {
			live = append(live, dir)
		}
	}
	return live
}
