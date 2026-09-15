//go:build unix

package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// grandchildScript backgrounds `sleep 300` (a grandchild standing in for the
// ffmpeg that yt-dlp forks), records its PID in pidFile, then runs tail.
func grandchildScript(pidFile, tail string) string {
	return fmt.Sprintf(`sleep 300 & echo $! > %q; %s`, pidFile, tail)
}

func readPID(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(b))); convErr == nil && pid > 0 {
				t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild PID never written to %s", pidFile)
	return 0
}

// processAlive reports whether pid is a live, non-zombie process.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	if i := strings.LastIndexByte(string(stat), ')'); i >= 0 && i+2 < len(stat) {
		return stat[i+2] != 'Z'
	}
	return true
}

func assertGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild PID %d is still running after DumpJSON returned", pid)
}

func dumpAsync(ctx context.Context, script string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, _, err := DumpJSON(ctx, []string{"-c", script})
		done <- err
	}()
	return done
}

func awaitBounded(t *testing.T, done <-chan error, limit time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("DumpJSON did not return within %s", limit)
		return nil
	}
}

// TestDumpJSON_CancelKillsGrandchildPID: cancelling the context while yt-dlp
// waits on its ffmpeg grandchild must kill the whole process group.
func TestDumpJSON_CancelKillsGrandchildPID(t *testing.T) {
	withBinary(t, "sh")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := dumpAsync(ctx, grandchildScript(pidFile, "wait"))
	pid := readPID(t, pidFile)
	cancel()
	if err := awaitBounded(t, done, 10*time.Second); err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}
	assertGone(t, pid)
}

// TestDumpJSON_ChildExitReapsOrphanedGrandchild: yt-dlp exits (crash, OOM
// kill) while its grandchild still holds stdout/stderr. DumpJSON must neither
// block until the grandchild exits nor leave it running.
func TestDumpJSON_ChildExitReapsOrphanedGrandchild(t *testing.T) {
	withBinary(t, "sh")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	done := dumpAsync(context.Background(), grandchildScript(pidFile, "exit 0"))
	pid := readPID(t, pidFile)
	if err := awaitBounded(t, done, 10*time.Second); err == nil {
		t.Fatal("expected an error for output left open by an orphaned grandchild, got nil")
	}
	assertGone(t, pid)
}
