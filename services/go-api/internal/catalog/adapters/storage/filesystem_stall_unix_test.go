//go:build unix

package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO with no writer blocks open(2) for reading indefinitely, the same way
// a stalled network mount does, which makes it a real stand-in for #1064.
const stallHangGuard = 5 * time.Second

func makeStalledAudio(t *testing.T, dir, ref string) {
	t.Helper()
	path := filepath.Join(dir, ref)
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	// Release the abandoned open(2) so its goroutine exits: attach a writer
	// once the stuck reader is present (ENXIO until then), then hang up.
	t.Cleanup(func() { releaseStalledReader(t, path) })
}

func releaseStalledReader(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(stallHangGuard)
	for time.Now().Before(deadline) {
		w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			w.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("no stalled reader to release on %s", path)
}

type streamResult struct {
	err error
}

func streamWithHangGuard(t *testing.T, ctx context.Context, store *FilesystemAudioStore, ref string) error {
	t.Helper()
	done := make(chan streamResult, 1)
	go func() {
		rc, _, err := store.Stream(ctx, ref)
		if err == nil {
			rc.Close()
		}
		done <- streamResult{err: err}
	}()
	select {
	case res := <-done:
		return res.err
	case <-time.After(stallHangGuard):
		t.Fatalf("Stream on a stalled file still blocked after %s: context is ignored", stallHangGuard)
		return nil
	}
}

func TestFilesystemAudioStore_Stream_StalledOpenHonoursCallerDeadline(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	makeStalledAudio(t, dir, "stalled.opus")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := streamWithHangGuard(t, ctx, store, "stalled.opus")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stream returned after %s, want roughly the 100ms caller deadline", elapsed)
	}
}

func TestFilesystemAudioStore_Stream_StalledOpenCappedWithoutCallerDeadline(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	store.opTimeout = 100 * time.Millisecond
	makeStalledAudio(t, dir, "stalled.opus")

	err := streamWithHangGuard(t, context.Background(), store, "stalled.opus")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected opTimeout to cap the open with context.DeadlineExceeded, got %v", err)
	}
}
