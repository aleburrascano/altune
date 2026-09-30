package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

func useTempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	if os.TempDir() != root {
		t.Fatalf("os.TempDir() = %q, want the test root %q", os.TempDir(), root)
	}
	return root
}

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

func TestSweepStaleTempDirsReapsAgedCookieCopyDirKeepsFresh(t *testing.T) {
	tempRoot := useTempRoot(t)
	aged := agedDir(t, tempRoot, tempDirPrefix+"cookies-aged", maxLiveTempAge+time.Hour)
	fresh := agedDir(t, tempRoot, tempDirPrefix+"cookies-fresh", time.Minute)

	SweepStaleTempDirs()

	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Errorf("aged cookie copy dir: Stat err = %v, want not-exist", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh cookie copy dir was removed: %v", err)
	}
}
