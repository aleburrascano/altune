package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type recordingWorkspace struct {
	cutoffs []time.Time
}

func (r *recordingWorkspace) Create(string) (string, error) { return "", nil }
func (r *recordingWorkspace) Remove(string) error           { return nil }
func (r *recordingWorkspace) Size(string) (int64, error)    { return 0, nil }
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

func (osWorkspace) Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

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

func (r *callRecordingWorkspace) Size(path string) (int64, error) { return r.inner.Size(path) }

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

func TestCleanupTemp_RemovesTempDirRootOfNestedLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "altune-acquire-x")
	nested := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "track.flac")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	CleanupTemp(context.Background(), osWorkspace{}, &AcquisitionContext{TempPath: file, TempDir: root})

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("temp root %q should be removed entirely, stat err = %v", root, err)
	}
}

func TestCleanupTemp_WithoutTempDirRemovesOnlyFileParent(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "sub")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "track.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	CleanupTemp(context.Background(), osWorkspace{}, &AcquisitionContext{TempPath: file})

	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Errorf("file parent %q should be removed, stat err = %v", parent, err)
	}
	if _, err := os.Stat(outer); err != nil {
		t.Errorf("directory above the parent must survive: %v", err)
	}
}

func TestCleanupTemp_EmptyTempPathRemovesNothing(t *testing.T) {
	ws := newCallRecordingWorkspace()

	CleanupTemp(context.Background(), ws, &AcquisitionContext{TempDir: t.TempDir()})
	CleanupTemp(context.Background(), ws, &AcquisitionContext{})

	if got := ws.removedPaths(); len(got) != 0 {
		t.Errorf("Remove calls = %v, want 0", got)
	}
}

func TestCleanupTemp_RemovesTempDirNotFileParent(t *testing.T) {
	ws := newCallRecordingWorkspace()
	root := filepath.Join(t.TempDir(), "altune-acquire-x")
	file := filepath.Join(root, "Artist", "Album", "track.flac")

	CleanupTemp(context.Background(), ws, &AcquisitionContext{TempPath: file, TempDir: root})

	if got := ws.removedPaths(); len(got) != 1 || got[0] != root {
		t.Errorf("removed = %v, want exactly [%s]", got, root)
	}
}

func TestCleanupTemp_WithoutTempDirRemovesFileParent(t *testing.T) {
	ws := newCallRecordingWorkspace()
	file := filepath.Join(t.TempDir(), "sub", "track.mp3")

	CleanupTemp(context.Background(), ws, &AcquisitionContext{TempPath: file})

	if got := ws.removedPaths(); len(got) != 1 || got[0] != filepath.Dir(file) {
		t.Errorf("removed = %v, want exactly [%s]", got, filepath.Dir(file))
	}
}
