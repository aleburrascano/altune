package service_test

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const maxLiveTempAge = service.AcquireTimeout + 15*time.Minute

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
	service.SweepStaleTempDirs(fake)
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

	service.SweepStaleTempDirs(fake)

	if len(fake.cutoffs) != 1 {
		t.Fatalf("SweepAbandoned calls = %d, want 1", len(fake.cutoffs))
	}
	if age := time.Since(fake.cutoffs[0]); age < service.AcquireTimeout {
		t.Errorf("cutoff is %v in the past, want at least AcquireTimeout %v", age, service.AcquireTimeout)
	}
}

func TestSweepStaleTempDirsRepeatedCallsNeverMoveCutoffBack(t *testing.T) {
	fake := &recordingWorkspace{}

	service.SweepStaleTempDirs(fake)
	service.SweepStaleTempDirs(fake)

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

type callRecordingWorkspace struct {
	inner ports.TempWorkspace

	mu      sync.Mutex
	removed []string
}

func newCallRecordingWorkspace() *callRecordingWorkspace {
	return &callRecordingWorkspace{inner: osWorkspace{}}
}

func (r *callRecordingWorkspace) Create(pattern string) (string, error) {
	return r.inner.Create(pattern)
}

func (r *callRecordingWorkspace) Remove(path string) error {
	r.mu.Lock()
	r.removed = append(r.removed, path)
	r.mu.Unlock()
	return r.inner.Remove(path)
}

func (r *callRecordingWorkspace) SweepAbandoned(t time.Time) int { return r.inner.SweepAbandoned(t) }

func (r *callRecordingWorkspace) Size(path string) (int64, error) { return r.inner.Size(path) }

func (r *callRecordingWorkspace) removedPaths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.removed...)
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

	service.CleanupTemp(context.Background(), osWorkspace{}, &service.AcquisitionContext{TempPath: file, TempDir: root})

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

	service.CleanupTemp(context.Background(), osWorkspace{}, &service.AcquisitionContext{TempPath: file})

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("temp file %q should be removed, stat err = %v", file, err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Errorf("the file's directory must survive: %v", err)
	}
	if _, err := os.Stat(outer); err != nil {
		t.Errorf("directory above the parent must survive: %v", err)
	}
}

func TestCleanupTemp_WithoutTempDirRemovesOnlyTheFile(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "track.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	service.CleanupTemp(context.Background(), osWorkspace{}, &service.AcquisitionContext{TempPath: file})

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("temp file should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Errorf("the file's directory must survive: %v", err)
	}
}

func TestCleanupTemp_WithoutAWorkspaceRemovesNothingAndDoesNotPanic(t *testing.T) {
	service.CleanupTemp(context.Background(), nil, &service.AcquisitionContext{TempPath: "/x/track.mp3", TempDir: "/x"})
}

func TestCleanupTemp_EmptyTempPathRemovesNothing(t *testing.T) {
	ws := newCallRecordingWorkspace()

	service.CleanupTemp(context.Background(), ws, &service.AcquisitionContext{TempDir: t.TempDir()})
	service.CleanupTemp(context.Background(), ws, &service.AcquisitionContext{})

	if got := ws.removedPaths(); len(got) != 0 {
		t.Errorf("Remove calls = %v, want 0", got)
	}
}

func TestCleanupTemp_RemovesTempDirNotFileParent(t *testing.T) {
	ws := newCallRecordingWorkspace()
	root := filepath.Join(t.TempDir(), "altune-acquire-x")
	file := filepath.Join(root, "Artist", "Album", "track.flac")

	service.CleanupTemp(context.Background(), ws, &service.AcquisitionContext{TempPath: file, TempDir: root})

	if got := ws.removedPaths(); len(got) != 1 || got[0] != root {
		t.Errorf("removed = %v, want exactly [%s]", got, root)
	}
}

func TestCleanupTemp_WithoutTempDirRemovesFileParent(t *testing.T) {
	ws := newCallRecordingWorkspace()
	file := filepath.Join(t.TempDir(), "sub", "track.mp3")

	service.CleanupTemp(context.Background(), ws, &service.AcquisitionContext{TempPath: file})

	if got := ws.removedPaths(); len(got) != 1 || got[0] != file {
		t.Errorf("removed = %v, want exactly [%s]", got, file)
	}
}
