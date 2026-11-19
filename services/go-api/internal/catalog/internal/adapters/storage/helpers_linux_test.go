package storage_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

var otherFilesystemRoots = []string{"/dev/shm"}

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
	base, ok := deviceOf(than)
	if !ok {
		t.Fatalf("stat %s", than)
	}
	for _, root := range otherFilesystemRoots {
		if dir, ok := tempDirOnOtherDevice(t, root, base); ok {
			return dir
		}
	}
	t.Skipf("no filesystem other than the one holding %s to move across", than)
	return ""
}

func tempDirOnOtherDevice(t *testing.T, root string, base uint64) (string, bool) {
	t.Helper()
	dev, ok := deviceOf(root)
	if !ok || dev == base {
		return "", false
	}
	dir, err := os.MkdirTemp(root, "storage-test-")
	if err != nil {
		return "", false
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir, true
}

func deviceOf(path string) (uint64, bool) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, false
	}
	return st.Dev, true
}
