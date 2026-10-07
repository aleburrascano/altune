package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
