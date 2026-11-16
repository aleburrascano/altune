package history_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func keepsAPoint(t *testing.T, store history.Database) bool {
	t.Helper()
	store.Record("reliability", "up", core.Point{At: base, Value: 1})
	points, err := store.Query("reliability", "up", base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return len(points) == 1
}

func TestAShortSchemaTimeoutMakesOpenFallBackToUnavailable(t *testing.T) {
	logs := captureLog(t)
	store := history.Open(filepath.Join(t.TempDir(), "history.db"), history.WithSchemaTimeout(time.Nanosecond))
	t.Cleanup(func() { _ = store.Close() })

	if keepsAPoint(t, store) {
		t.Fatal("Open under a 1ns schema timeout kept a point, want the unavailable store")
	}
	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Fatalf("log does not say history is unavailable: %s", logs.String())
	}
}

func TestTheDefaultSchemaTimeoutKeepsHistoryAvailable(t *testing.T) {
	store := history.Open(filepath.Join(t.TempDir(), "history.db"))
	t.Cleanup(func() { _ = store.Close() })

	if !keepsAPoint(t, store) {
		t.Fatal("Open with the default schema timeout lost a point, want a store on disk")
	}
}

func TestANonPositiveSchemaTimeoutKeepsTheDefault(t *testing.T) {
	store := history.Open(filepath.Join(t.TempDir(), "history.db"), history.WithSchemaTimeout(0))
	t.Cleanup(func() { _ = store.Close() })

	if !keepsAPoint(t, store) {
		t.Fatal("Open with a zero schema timeout lost a point, want the default timeout kept and a store on disk")
	}
}

func TestPointsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	first := history.Open(path)
	first.Record("reliability", "latency_ms", core.Point{At: base, Value: 42.5})
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := history.Open(path)
	t.Cleanup(func() { _ = second.Close() })
	points, err := second.Query("reliability", "latency_ms", base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(points) != 1 || points[0].Value != 42.5 || !points[0].At.Equal(base) {
		t.Fatalf("points after reopen = %+v, want the one written before", points)
	}
}

func TestCorruptFileStartsUnavailableAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	if err := os.WriteFile(path, bytes.Repeat([]byte("not a sqlite database "), 400), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	logs := captureLog(t)

	store := history.Open(path)
	store.Record("reliability", "up", core.Point{At: base, Value: 1})

	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Fatalf("log does not say history is unavailable: %s", logs.String())
	}
	names, err := store.Names("reliability")
	if err != nil || len(names) != 0 {
		t.Fatalf("Names on an unavailable store = %v, %v; want empty, nil", names, err)
	}
	points, err := store.Query("reliability", "up", base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil || len(points) != 0 {
		t.Fatalf("Query on an unavailable store = %v, %v; want empty, nil", points, err)
	}
}

func TestMissingDirectoryStartsUnavailable(t *testing.T) {
	logs := captureLog(t)
	store := history.Open(filepath.Join(t.TempDir(), "absent", "history.db"))

	if keepsAPoint(t, store) {
		t.Fatal("Open on a missing directory kept a point, want the unavailable store")
	}
	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Fatalf("log does not say history is unavailable: %s", logs.String())
	}
}

func TestMissingFileStartsEmptyOnDisk(t *testing.T) {
	store := history.Open(filepath.Join(t.TempDir(), "history.db"))
	t.Cleanup(func() { _ = store.Close() })

	names, err := store.Names("reliability")
	if err != nil || len(names) != 0 {
		t.Fatalf("fresh store Names = %v, %v; want empty", names, err)
	}
	if !keepsAPoint(t, store) {
		t.Fatal("Open on a missing file lost a point, want a fresh store on disk")
	}
}

func TestAnUnavailableHistoryRestoresNothingAndAcceptsWrites(t *testing.T) {
	captureLog(t)
	store := history.Open(filepath.Join(t.TempDir(), "absent", "history.db"))

	store.AppendSignals("heartbeat", "ticks", 10, []core.Signal{{At: base, Kind: "tick", Text: "tick-0"}})
	loaded, err := store.LoadSignals(context.Background(), "heartbeat", "ticks", 10)

	if err != nil || len(loaded) != 0 {
		t.Fatalf("unavailable LoadSignals = %+v, %v; want nothing", loaded, err)
	}
	store.RunPruner(context.Background())
	if err := store.Close(); err != nil {
		t.Fatalf("unavailable Close = %v, want nil", err)
	}
}
