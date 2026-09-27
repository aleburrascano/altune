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
	t.Fatalf("grandchild PID %d is still running after the runner returned", pid)
}

func runAsync(timeout time.Duration, script string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, _, err := RunWithTimeout(context.Background(), timeout, "sh", "-c", script)
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
		t.Fatalf("RunWithTimeout did not return within %s", limit)
		return nil
	}
}

func TestRunWithTimeout_TimeoutKillsGrandchildPID(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	done := runAsync(500*time.Millisecond, grandchildScript(pidFile, "wait"))
	pid := readPID(t, pidFile)
	if err := awaitBounded(t, done, 10*time.Second); err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	assertGone(t, pid)
}

func TestRunWithTimeout_ChildExitReapsOrphanedGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	done := runAsync(time.Minute, grandchildScript(pidFile, "exit 0"))
	pid := readPID(t, pidFile)
	if err := awaitBounded(t, done, 10*time.Second); err == nil {
		t.Fatal("expected an error for output left open by an orphaned grandchild, got nil")
	}
	assertGone(t, pid)
}
