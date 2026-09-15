//go:build unix

package execcmd

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

// grandchildScript returns a shell script that backgrounds `sleep 300` (a
// grandchild standing in for ffmpeg), records its PID in pidFile, and then
// runs tail (e.g. "wait" to block, or "" to exit and orphan it).
func grandchildScript(pidFile, tail string) string {
	return fmt.Sprintf(`sleep 300 & echo $! > %q; %s`, pidFile, tail)
}

// readPID polls pidFile until the shell has written the grandchild's PID.
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

// processAlive reports whether pid names a live (non-zombie) process. A killed
// grandchild may linger briefly as a zombie until its new parent reaps it, so a
// zombie counts as gone.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		// No procfs (e.g. darwin) or the process just vanished: fall back to
		// kill(pid, 0), which succeeded above unless it raced an exit.
		return syscall.Kill(pid, 0) == nil
	}
	// Field 3 is the state, after the parenthesised command name.
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
	t.Fatalf("grandchild PID %d is still running after the runner returned", pid)
}

// runAsync starts RunWithTimeout in a goroutine and returns its error channel.
func runAsync(timeout time.Duration, script string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, _, err := RunWithTimeout(context.Background(), timeout, "sh", "-c", script)
		done <- err
	}()
	return done
}

// awaitBounded fails the test if the runner does not return within limit (a
// runner blocked on an orphan's still-open pipe would not).
func awaitBounded(t *testing.T, done <-chan error, limit time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("RunWithTimeout did not return within %s", limit)
		return nil
	}
}

// TestRunWithTimeout_TimeoutKillsGrandchildPID: the context deadline fires
// while the child is blocked waiting on its grandchild; the whole process group
// must be killed, not just the direct child.
func TestRunWithTimeout_TimeoutKillsGrandchildPID(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	done := runAsync(500*time.Millisecond, grandchildScript(pidFile, "wait"))
	pid := readPID(t, pidFile)
	if err := awaitBounded(t, done, 10*time.Second); err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	assertGone(t, pid)
}

// TestRunWithTimeout_ChildExitReapsOrphanedGrandchild: the direct child exits
// (as yt-dlp does when it crashes or is OOM-killed) while its grandchild still
// holds the stdout/stderr pipes. The runner must neither block until the
// grandchild exits nor leave it running.
func TestRunWithTimeout_ChildExitReapsOrphanedGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	done := runAsync(time.Minute, grandchildScript(pidFile, "exit 0"))
	pid := readPID(t, pidFile)
	if err := awaitBounded(t, done, 10*time.Second); err == nil {
		t.Fatal("expected an error for output left open by an orphaned grandchild, got nil")
	}
	assertGone(t, pid)
}
