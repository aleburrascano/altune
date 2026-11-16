package sqlite_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history/internal/adapters/sqlite"
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type store struct {
	*sqlite.Disk
	raw *sql.DB
}

func rawConn(t testing.TB, path string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func openStore(t testing.TB, path string, opts ...sqlite.Option) *store {
	t.Helper()
	d, err := sqlite.Open(path, opts...)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &store{Disk: d, raw: rawConn(t, path)}
}

func openTemp(t *testing.T, opts ...sqlite.Option) (*store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	deadline := time.Now().Add(2 * time.Minute)
	for {
		d, err := sqlite.Open(path, opts...)
		if err == nil {
			t.Cleanup(func() { _ = d.Close() })
			return &store{Disk: d, raw: rawConn(t, path)}, path
		}
		if time.Now().After(deadline) {
			t.Fatalf("sqlite.Open: %v", err)
		}
	}
}

func countRows(t *testing.T, d *store, bucket, series string) int {
	t.Helper()
	var n int
	if err := d.raw.QueryRow("SELECT COUNT(*) FROM points WHERE bucket = ? AND series = ?", bucket, series).Scan(&n); err != nil {
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
	d, _ := openTemp(t, sqlite.WithCap(rowCap))

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
	d, _ := openTemp(t, sqlite.WithCap(5))
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
	d, _ := openTemp(t, sqlite.WithClock(func() time.Time { return now }))
	d.Record("reliability", "up", core.Point{At: now.Add(-sqlite.DefaultRetention - time.Minute), Value: 1})
	d.Record("reliability", "up", core.Point{At: now.Add(-sqlite.DefaultRetention + time.Minute), Value: 2})

	if err := d.Prune(context.Background()); err != nil {
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
	wide, err := sqlite.Open(path, sqlite.WithCap(20))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	for i := range 20 {
		wide.Record("reliability", "up", core.Point{At: base.Add(time.Duration(i) * time.Second), Value: 1})
	}
	if err := wide.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	narrow := openStore(t, path, sqlite.WithCap(5), sqlite.WithClock(func() time.Time { return base }))
	if err := narrow.Prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if got := countRows(t, narrow, "reliability", "up"); got != 5 {
		t.Fatalf("rows after prune = %d, want 5", got)
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
