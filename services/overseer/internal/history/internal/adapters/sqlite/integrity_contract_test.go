package sqlite_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history/internal/adapters/sqlite"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlowIntegrityCheckKeepsHistoryOnDisk(t *testing.T) {
	logs := captureLog(t)

	d, _ := openTemp(t, sqlite.WithCheckTimeout(time.Nanosecond))
	d.Record("reliability", "up", core.Point{At: base, Value: 1})

	if got := countRows(t, d, "reliability", "up"); got != 1 {
		t.Fatalf("rows after a timed-out integrity check = %d, want 1", got)
	}
	if !strings.Contains(logs.String(), "history.integrity_check_timed_out") {
		t.Fatalf("log does not name the timed-out integrity check: %s", logs.String())
	}
}

func TestCorruptFileStillFailsUnderTheIntegrityTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	if err := os.WriteFile(path, bytes.Repeat([]byte("not a sqlite database "), 400), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	if _, err := sqlite.Open(path); err == nil {
		t.Fatal("sqlite.Open accepted a corrupt file")
	}
}
