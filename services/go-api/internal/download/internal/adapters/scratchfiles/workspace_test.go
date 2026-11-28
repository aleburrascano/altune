package scratchfiles_test

import (
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testMaxAge = time.Hour

const tempDirPrefix = "altune-acquire-"

func newWorkspace() ports.ScratchFolder { return scratchfiles.New() }

func TestCreateNamesDirUnderTempRoot(t *testing.T) {
	useTempRoot(t)
	ws := newWorkspace()
	for pattern, wantPrefix := range map[string]string{
		"*":         "altune-acquire-",
		"preview-*": "altune-acquire-preview-",
	} {
		dir, err := ws.Create(pattern)
		if err != nil {
			t.Fatalf("Create(%q): %v", pattern, err)
		}
		if filepath.Dir(dir) != os.TempDir() {
			t.Errorf("Create(%q) parent = %q, want %q", pattern, filepath.Dir(dir), os.TempDir())
		}
		if !strings.HasPrefix(filepath.Base(dir), wantPrefix) {
			t.Errorf("Create(%q) base = %q, want prefix %q", pattern, filepath.Base(dir), wantPrefix)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("Create(%q) did not make a directory: %v", pattern, err)
		}
	}
}

func TestRemoveDeletesTreeAndFile(t *testing.T) {
	root := useTempRoot(t)
	ws := newWorkspace()

	tree := filepath.Join(root, "tree")
	nested := filepath.Join(tree, "Artist", "Album")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "track.flac"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "lone.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{tree, file} {
		if err := ws.Remove(path); err != nil {
			t.Errorf("Remove(%s): %v", path, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: Stat err = %v, want not-exist", path, err)
		}
	}
}

func TestRemoveMissingPathIsNil(t *testing.T) {
	root := useTempRoot(t)
	if err := newWorkspace().Remove(filepath.Join(root, "missing")); err != nil {
		t.Errorf("Remove(missing) = %v, want nil", err)
	}
}

func TestSweepAbandonedEmptyRootReturnsZero(t *testing.T) {
	useTempRoot(t)
	if got := newWorkspace().SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge)); got != 0 {
		t.Errorf("SweepAbandoned on empty root = %d, want 0", got)
	}
}

func TestSweepAbandonedRemovesAgedKeepsFresh(t *testing.T) {
	root := useTempRoot(t)
	aged := agedDir(t, root, tempDirPrefix+"abandoned", testMaxAge+time.Hour)
	fresh := agedDir(t, root, tempDirPrefix+"live", testMaxAge-time.Minute)

	if got := newWorkspace().SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge)); got != 1 {
		t.Errorf("SweepAbandoned = %d, want 1", got)
	}
	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Errorf("aged dir: Stat err = %v, want not-exist", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh dir was removed: %v", err)
	}
}

func TestSweepAbandonedRefusesLookAlikes(t *testing.T) {
	root := useTempRoot(t)
	foreignDir := agedDir(t, root, "unrelated-service-cache", testMaxAge+time.Hour)
	namedFile := agedFile(t, root, tempDirPrefix+"notadir", testMaxAge+time.Hour)

	if got := newWorkspace().SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge)); got != 0 {
		t.Errorf("SweepAbandoned = %d, want 0", got)
	}
	for _, path := range []string{foreignDir, namedFile} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("entry outside the sweep's claim was removed: %s: %v", path, err)
		}
	}
}

func TestSweepAbandonedBetweenTicks(t *testing.T) {
	root := useTempRoot(t)
	ws := newWorkspace()
	dir := agedDir(t, root, tempDirPrefix+"job", time.Minute)
	agedDir(t, root, tempDirPrefix+"old", testMaxAge+time.Hour)

	if got := ws.SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge)); got != 1 {
		t.Fatalf("first sweep = %d, want 1", got)
	}
	if got := ws.SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge)); got != 0 {
		t.Errorf("second sweep right after = %d, want 0", got)
	}
	backdate(t, dir, testMaxAge+time.Hour)
	if got := ws.SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge)); got != 1 {
		t.Errorf("sweep after backdating = %d, want 1", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("backdated dir: Stat err = %v, want not-exist", err)
	}
}

func TestSweepAbandonedLogsNothingWithoutStaleDirs(t *testing.T) {
	root := useTempRoot(t)
	agedDir(t, root, tempDirPrefix+"live", testMaxAge-time.Minute)
	logs := logging.CaptureJSON(t, slog.LevelDebug)

	newWorkspace().SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge))

	if logs.Len() != 0 {
		t.Errorf("sweep with no stale dirs logged:\n%s", logs.String())
	}
}

func TestSweepAbandonedLogsTheSweptCount(t *testing.T) {
	root := useTempRoot(t)
	agedDir(t, root, tempDirPrefix+"old", testMaxAge+time.Hour)
	logs := logging.CaptureJSON(t, slog.LevelDebug)

	newWorkspace().SweepAbandoned(context.Background(), time.Now().Add(-testMaxAge))

	var record struct {
		Level   string `json:"level"`
		Msg     string `json:"msg"`
		Removed int    `json:"removed"`
	}
	if err := json.Unmarshal([]byte(logs.String()), &record); err != nil {
		t.Fatalf("expected exactly one JSON log line, got %q: %v", logs.String(), err)
	}
	if record.Level != "INFO" || record.Msg != "acquisition.stale_temp_dirs_swept" || record.Removed != 1 {
		t.Errorf("log = %+v, want INFO acquisition.stale_temp_dirs_swept removed=1", record)
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
