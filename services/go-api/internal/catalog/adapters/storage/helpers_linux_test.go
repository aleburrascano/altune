package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

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
