package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSweepStaleTempDirsReapsAbandonedKeepsLive proves #1978: a temp dir left by
// a hard kill (SIGKILL, OOM, a redeploy past the drain deadline), where
// step_download's defers never run, is reaped at startup, while a dir a running
// job could still own survives the sweep.
func TestSweepStaleTempDirsReapsAbandonedKeepsLive(t *testing.T) {
	tempRoot := useTempRoot(t)
	abandoned := agedDir(t, tempRoot, tempDirPrefix+"abandoned", maxLiveTempAge+time.Hour)
	live := agedDir(t, tempRoot, tempDirPrefix+"live", maxLiveTempAge-time.Minute)

	SweepStaleTempDirs()

	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Errorf("abandoned temp dir: Stat err = %v, want not-exist", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Errorf("temp dir a live job could own was removed: %v", err)
	}
}

// TestSweepStaleTempDirsLeavesForeignEntriesAlone pins the blast radius: the
// sweep deletes inside a world-writable temp dir, so age alone must never be
// enough — an old entry that is not one of our directories stays.
func TestSweepStaleTempDirsLeavesForeignEntriesAlone(t *testing.T) {
	tempRoot := useTempRoot(t)
	foreignDir := agedDir(t, tempRoot, "unrelated-service-cache", maxLiveTempAge+time.Hour)
	ourNamedFile := agedFile(t, tempRoot, tempDirPrefix+"notadir", maxLiveTempAge+time.Hour)

	SweepStaleTempDirs()

	for _, path := range []string{foreignDir, ourNamedFile} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("entry outside the sweep's claim was removed: %s: %v", path, err)
		}
	}
}

// useTempRoot points os.TempDir() at a directory of this test's own, so the
// sweep can never reach the real temp dir of the machine running the suite.
func useTempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	if os.TempDir() != root {
		t.Fatalf("os.TempDir() = %q, want the test root %q", os.TempDir(), root)
	}
	return root
}

// agedDir creates a directory holding a downloaded file and backdates it,
// standing in for what an earlier acquisition left in the temp root. The file is
// what holds the sweep to removing a dir that still carries a download.
func agedDir(t *testing.T, root, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(path, "track.m4a"), []byte("audio"), 0o600); err != nil {
		t.Fatalf("write downloaded file in %s: %v", name, err)
	}
	backdate(t, path, age)
	return path
}

func agedFile(t *testing.T, root, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	backdate(t, path, age)
	return path
}

func backdate(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}
