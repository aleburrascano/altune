package history

import (
	"altune/overseer/internal/core"
	"bytes"
	"context"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func openTemp(t *testing.T, opts ...Option) (*disk, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := openDisk(path, opts...)
	if err != nil {
		t.Fatalf("openDisk: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, path
}

func countRows(t *testing.T, d *disk, bucket, series string) int {
	t.Helper()
	var n int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM points WHERE bucket = ? AND series = ?", bucket, series).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestWritingPastTheCapLeavesExactlyTheCapNewestPoints(t *testing.T) {
	const rowCap, extra = 50, 17
	d, _ := openTemp(t, WithCap(rowCap))

	for i := range rowCap + extra {
		d.Record("reliability", "up", core.Point{At: base.Add(time.Duration(i) * time.Second), Value: float64(i)})
	}

	if got := countRows(t, d, "reliability", "up"); got != rowCap {
		t.Fatalf("rows after %d writes = %d, want exactly the cap %d", rowCap+extra, got, rowCap)
	}
	points, err := d.Query("reliability", "up", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(points) != rowCap {
		t.Fatalf("Query returned %d points, want %d", len(points), rowCap)
	}
	if first := points[0].Value; first != float64(extra) {
		t.Fatalf("oldest retained value = %v, want %v (the oldest points are the ones evicted)", first, float64(extra))
	}
}

func TestCapIsPerSeries(t *testing.T) {
	d, _ := openTemp(t, WithCap(5))
	for i := range 8 {
		d.Record("reliability", "up", core.Point{At: base.Add(time.Duration(i) * time.Second), Value: 1})
	}
	d.Record("reliability", "latency_ms", core.Point{At: base, Value: 12})

	if got := countRows(t, d, "reliability", "latency_ms"); got != 1 {
		t.Fatalf("latency_ms rows = %d, want 1: one series filling its cap must not evict another", got)
	}
}

func TestPruneDropsPointsOlderThanRetention(t *testing.T) {
	now := base.Add(30 * 24 * time.Hour)
	d, _ := openTemp(t, WithClock(func() time.Time { return now }))
	d.Record("reliability", "up", core.Point{At: now.Add(-DefaultRetention - time.Minute), Value: 1})
	d.Record("reliability", "up", core.Point{At: now.Add(-DefaultRetention + time.Minute), Value: 2})

	if err := d.prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}

	points, err := d.Query("reliability", "up", now.Add(-60*24*time.Hour), now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("points after prune = %+v, want only the one inside 7d", points)
	}
}

func TestPruneTrimsASeriesLeftOverALoweredCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	wide, err := openDisk(path, WithCap(20))
	if err != nil {
		t.Fatalf("openDisk: %v", err)
	}
	for i := range 20 {
		wide.Record("reliability", "up", core.Point{At: base.Add(time.Duration(i) * time.Second), Value: 1})
	}
	if err := wide.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	narrow, err := openDisk(path, WithCap(5), WithClock(func() time.Time { return base }))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = narrow.Close() })
	if err := narrow.prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if got := countRows(t, narrow, "reliability", "up"); got != 5 {
		t.Fatalf("rows after prune = %d, want 5", got)
	}
}

func TestPointsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	first := Open(path)
	first.Record("reliability", "latency_ms", core.Point{At: base, Value: 42.5})
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := Open(path)
	t.Cleanup(func() { _ = second.Close() })
	points, err := second.Query("reliability", "latency_ms", base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(points) != 1 || points[0].Value != 42.5 || !points[0].At.Equal(base) {
		t.Fatalf("points after reopen = %+v, want the one written before", points)
	}
}

func TestQueryReturnsOnlyTheWindowOldestFirst(t *testing.T) {
	d, _ := openTemp(t)
	for i := range 5 {
		d.Record("reliability", "up", core.Point{At: base.Add(time.Duration(4-i) * time.Minute), Value: float64(4 - i)})
	}

	points, err := d.Query("reliability", "up", base.Add(time.Minute), base.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var got []float64
	for _, p := range points {
		got = append(got, p.Value)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("window values = %v, want [1 2 3]", got)
	}
}

func TestNamesListsOnlyTheBucketsOwnSeries(t *testing.T) {
	d, _ := openTemp(t)
	d.Record("reliability", "up", core.Point{At: base, Value: 1})
	d.Record("reliability", "latency_ms", core.Point{At: base, Value: 9})
	d.Record("cost", "spend", core.Point{At: base, Value: 3})

	names, err := d.Names("reliability")
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if strings.Join(names, ",") != "latency_ms,up" {
		t.Fatalf("Names = %v, want [latency_ms up]", names)
	}
}

func TestNonFiniteValueIsRefusedAndLoggedOnce(t *testing.T) {
	d, _ := openTemp(t)
	logs := captureLog(t)

	d.Record("reliability", "latency_ms", core.Point{At: base, Value: math.NaN()})
	d.Record("reliability", "latency_ms", core.Point{At: base, Value: math.Inf(1)})

	if got := countRows(t, d, "reliability", "latency_ms"); got != 0 {
		t.Fatalf("stored %d non-finite points, want 0", got)
	}
	if got := strings.Count(logs.String(), "history.write_failed"); got != 1 {
		t.Fatalf("write_failed logged %d times, want once per failing run", got)
	}
}

func TestCorruptFileStartsUnavailableAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	if err := os.WriteFile(path, bytes.Repeat([]byte("not a sqlite database "), 400), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	logs := captureLog(t)

	store := Open(path)
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
	store := Open(filepath.Join(t.TempDir(), "absent", "history.db"))

	if _, isUnavailable := store.(unavailable); !isUnavailable {
		t.Fatalf("Open on a missing directory returned %T, want the unavailable store", store)
	}
	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Fatalf("log does not say history is unavailable: %s", logs.String())
	}
}

func TestMissingFileStartsEmptyOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	store := Open(path)
	t.Cleanup(func() { _ = store.Close() })

	if _, isDisk := store.(*disk); !isDisk {
		t.Fatalf("Open on a missing file returned %T, want a fresh disk store", store)
	}
	names, err := store.Names("reliability")
	if err != nil || len(names) != 0 {
		t.Fatalf("fresh store Names = %v, %v; want empty", names, err)
	}
}

func TestRunPrunerStopsOnCancel(t *testing.T) {
	d, _ := openTemp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.RunPruner(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPruner did not return within 2s of cancel")
	}
}
