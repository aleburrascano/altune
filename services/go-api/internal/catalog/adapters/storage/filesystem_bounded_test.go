package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedFSCall_ReturnsCallResultUnchanged(t *testing.T) {
	sentinel := errors.New("boom")
	got, err := boundedFSCall(context.Background(), "op", func() (int, error) {
		return 7, sentinel
	}, nil)
	if got != 7 || !errors.Is(err, sentinel) || err.Error() != sentinel.Error() {
		t.Fatalf("got (%d, %v), want (7, the call's own error)", got, err)
	}
}

func TestBoundedFSCall_AlreadyDoneContextSkipsCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := boundedFSCall(ctx, "op", func() (struct{}, error) {
		called = true
		return struct{}{}, nil
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if called {
		t.Error("call ran despite an already-cancelled context")
	}
}

func TestBoundedFSCall_ExpiryDiscardsLateResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	release := make(chan struct{})
	discarded := make(chan int, 1)
	_, err := boundedFSCall(ctx, "op", func() (int, error) {
		<-release
		return 42, nil
	}, func(late int) { discarded <- late })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	close(release)
	select {
	case got := <-discarded:
		if got != 42 {
			t.Errorf("discarded %d, want the late result 42", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late result was never discarded: its resource would leak")
	}
}

func TestFilesystemAudioStore_CancelledContextDoesNotTouchDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "kept.opus"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Exists(ctx, "kept.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Exists: expected context.Canceled, got %v", err)
	}
	if _, _, err := store.Stream(ctx, "kept.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Stream: expected context.Canceled, got %v", err)
	}
	if err := store.Delete(ctx, "kept.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: expected context.Canceled, got %v", err)
	}
	src := filepath.Join(dir, "src.opus")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := store.Store(ctx, src, "stored.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Store: expected context.Canceled, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kept.opus")); err != nil {
		t.Errorf("Delete with a cancelled context removed the file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stored.opus")); !os.IsNotExist(err) {
		t.Errorf("Store with a cancelled context moved the file: %v", err)
	}
}
