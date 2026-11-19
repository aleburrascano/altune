package storage_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func makeFifo(t *testing.T, dir, ref string) string {
	t.Helper()
	path := filepath.Join(dir, ref)
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	return path
}

func makeStalledAudio(t *testing.T, dir, ref string) {
	t.Helper()
	path := makeFifo(t, dir, ref)
	t.Cleanup(func() { releaseStalledReader(t, path) })
}

func releaseStalledReader(t *testing.T, path string) {
	t.Helper()
	w, ok := openFifoWriter(path, stallHangGuard)
	if !ok {
		t.Logf("no stalled reader to release on %s", path)
		return
	}
	w.Close()
}

func openFifoWriter(path string, wait time.Duration) (*os.File, bool) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return w, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, false
}

func readerAppears(path string, within time.Duration) bool {
	w, ok := openFifoWriter(path, within)
	if ok {
		w.Close()
	}
	return ok
}

func lateReaderClosed(t *testing.T, path string) bool {
	t.Helper()
	w, ok := openFifoWriter(path, stallHangGuard)
	if !ok {
		t.Fatalf("the late open never reached %s", path)
	}
	defer w.Close()
	deadline := time.Now().Add(stallHangGuard)
	for time.Now().Before(deadline) {
		if _, err := w.Write([]byte{0}); errors.Is(err, syscall.EPIPE) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func otherFilesystemDir(t *testing.T, than string) string {
	t.Helper()
	t.Skipf("no second filesystem to move %s across on darwin", than)
	return ""
}
