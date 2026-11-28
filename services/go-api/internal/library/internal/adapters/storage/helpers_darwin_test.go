package storage_test

import (
	"altune/go-api/internal/shared/polltest"
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
	var writer *os.File
	opened := polltest.Within(wait, func() bool {
		w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return false
		}
		writer = w
		return true
	})
	return writer, opened
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
	return polltest.Within(stallHangGuard, func() bool {
		_, err := w.Write([]byte{0})
		return errors.Is(err, syscall.EPIPE)
	})
}

func otherFilesystemDir(t *testing.T, than string) string {
	t.Helper()
	t.Skipf("no second filesystem to move %s across on darwin", than)
	return ""
}
