package storage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

const stalledOpenGuard = 5 * time.Second

func TestFilesystemAudioStore_Stream_StalledOpenCappedWithoutCallerDeadline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stalled.opus")
	makeFifo(t, path)
	t.Cleanup(func() { releaseStalledOpen(t, path) })
	store := NewFilesystemAudioStore(dir)
	store.opTimeout = 100 * time.Millisecond

	err := streamWithinGuard(t, store, "stalled.opus")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected opTimeout to cap the open with context.DeadlineExceeded, got %v", err)
	}
}

func makeFifo(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("mkfifo", path).CombinedOutput()
	if err == nil {
		return
	}
	if runtime.GOOS == "linux" {
		t.Fatalf("mkfifo failed on linux, where coreutils ships it: %v %s", err, out)
	}
	t.Skipf("mkfifo unsupported here: %v %s", err, out)
}

func streamWithinGuard(t *testing.T, store *FilesystemAudioStore, ref string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		rc, _, err := store.Stream(context.Background(), ref)
		if err == nil {
			rc.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(stalledOpenGuard):
		t.Fatalf("Stream on a stalled open still blocked after %s: opTimeout does not cap it", stalledOpenGuard)
		return nil
	}
}

func releaseStalledOpen(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(stalledOpenGuard)
	for time.Now().Before(deadline) {
		if w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("no stalled reader to release on %s", path)
}
