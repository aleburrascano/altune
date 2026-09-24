package history

import (
	"altune/overseer/internal/core"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var rollupNow = base.Add(30 * 24 * time.Hour)

func fixedClock(now time.Time) Option {
	return WithClock(func() time.Time { return now })
}

func mustPrune(t *testing.T, d *disk) {
	t.Helper()
	if err := d.prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
}

func countWhere(t *testing.T, d *disk, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func mustMinutes(t *testing.T, d *disk, series string, from, to time.Time) []Minute {
	t.Helper()
	minutes, err := d.Minutes("reliability", series, from, to)
	if err != nil {
		t.Fatalf("Minutes: %v", err)
	}
	return minutes
}

func seedPoints(t testing.TB, d *disk, series string, points []core.Point) {
	t.Helper()
	tx, err := d.db.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	insert, err := tx.Prepare("INSERT INTO points (bucket, series, at, value) VALUES ('reliability', ?, ?, ?)")
	if err != nil {
		t.Fatalf("prepare seed: %v", err)
	}
	for _, p := range points {
		if _, err := insert.Exec(series, p.At.UnixMilli(), p.Value); err != nil {
			t.Fatalf("seed point: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
}

func threeSamplesPerMinute(from time.Time, minutes int) []core.Point {
	return threeSamplesEveryNthMinute(from, minutes, 1)
}

func threeSamplesEveryNthMinute(from time.Time, minutes, stride int) []core.Point {
	points := make([]core.Point, 0, 3*minutes/stride)
	for minute := 0; minute < minutes; minute += stride {
		start := from.Add(time.Duration(minute) * time.Minute)
		for sample := range 3 {
			points = append(points, core.Point{At: start.Add(time.Duration(sample) * 20 * time.Second), Value: float64(minute + sample)})
		}
	}
	return points
}

func TestPruneKeepsRawForTheLastDayAndFoldsOlderPointsIntoMinuteRollups(t *testing.T) {
	const days, stride = 3, 3
	start := rollupNow.Add(-days * 24 * time.Hour)
	d, _ := openTemp(t, fixedClock(rollupNow))
	seedPoints(t, d, "latency_ms", threeSamplesEveryNthMinute(start, days*24*60, stride))

	mustPrune(t, d)

	rawCutoff := rollupNow.Add(-RawRetention)
	if stale := countWhere(t, d, "SELECT COUNT(*) FROM points WHERE at < ?", rawCutoff.UnixMilli()); stale != 0 {
		t.Fatalf("%d raw points older than 24h survived the prune", stale)
	}
	if raw, want := countRows(t, d, "reliability", "latency_ms"), 24*60; raw != want {
		t.Fatalf("raw points kept = %d, want the %d from the last 24h", raw, want)
	}
	folded := mustMinutes(t, d, "latency_ms", start, rawCutoff.Add(-time.Millisecond))
	if want := (days - 1) * 24 * 60 / stride; len(folded) != want {
		t.Fatalf("rolled-up minutes = %d, want %d", len(folded), want)
	}
	for i, m := range folded {
		minute := i * stride
		want := Minute{At: start.Add(time.Duration(minute) * time.Minute), Min: float64(minute), Max: float64(minute + 2), Avg: float64(minute + 1)}
		if m != want {
			t.Fatalf("minute %d = %+v, want %+v", i, m, want)
		}
	}
}

func TestRollupRowsPerSeriesNeverExceedTheCap(t *testing.T) {
	const rowCap = 100
	d, _ := openTemp(t, WithCap(rowCap), fixedClock(rollupNow))
	oldest := rollupNow.Add(-3 * 24 * time.Hour)

	for round := range 3 {
		roundStart := oldest.Add(time.Duration(round*2*rowCap) * time.Minute)
		for _, series := range []string{"up", "latency_ms"} {
			seedPoints(t, d, series, threeSamplesPerMinute(roundStart, 2*rowCap))
		}
		mustPrune(t, d)

		for _, series := range []string{"up", "latency_ms"} {
			rollups := countWhere(t, d, "SELECT COUNT(*) FROM rollups WHERE bucket = 'reliability' AND series = ?", series)
			if rollups > rowCap {
				t.Fatalf("round %d: %s holds %d rollup rows, over the cap %d", round, series, rollups, rowCap)
			}
		}
	}
	newest := mustMinutes(t, d, "up", oldest, rollupNow)
	if len(newest) == 0 {
		t.Fatal("no rollups left after the prunes")
	}
	if last := newest[len(newest)-1].At; !last.Equal(oldest.Add(time.Duration(3*2*rowCap-1) * time.Minute)) {
		t.Fatalf("newest kept rollup = %v, want the newest minute written (the oldest are the ones evicted)", last)
	}
}

func TestPruneDropsRollupsThatAgePastRetention(t *testing.T) {
	now := rollupNow.Add(-5 * 24 * time.Hour)
	d, _ := openTemp(t, WithClock(func() time.Time { return now }))
	d.Record("reliability", "up", core.Point{At: rollupNow.Add(-DefaultRetention - time.Minute), Value: 1})
	d.Record("reliability", "up", core.Point{At: rollupNow.Add(-DefaultRetention + time.Minute), Value: 2})
	mustPrune(t, d)
	now = rollupNow

	mustPrune(t, d)

	minutes := mustMinutes(t, d, "up", rollupNow.Add(-60*24*time.Hour), rollupNow)
	if len(minutes) != 1 || minutes[0].Avg != 2 {
		t.Fatalf("minutes after prune = %+v, want only the one inside 7d", minutes)
	}
	if expired := countWhere(t, d, "SELECT COUNT(*) FROM rollups WHERE minute < ?", rollupNow.Add(-DefaultRetention).UnixMilli()); expired != 0 {
		t.Fatalf("%d rollups older than 7d survived the prune", expired)
	}
}

func TestPruningTwiceLeavesTheSameRollups(t *testing.T) {
	d, _ := openTemp(t, fixedClock(rollupNow))
	seedPoints(t, d, "up", threeSamplesPerMinute(rollupNow.Add(-26*time.Hour), 60))
	mustPrune(t, d)
	first := mustMinutes(t, d, "up", rollupNow.Add(-DefaultRetention), rollupNow)

	mustPrune(t, d)

	second := mustMinutes(t, d, "up", rollupNow.Add(-DefaultRetention), rollupNow)
	if len(first) != 60 || len(second) != len(first) || second[59] != first[59] {
		t.Fatalf("second prune changed the rollups: %d minutes then %d", len(first), len(second))
	}
}

func TestALatePointMergesIntoItsAlreadyFoldedMinute(t *testing.T) {
	d, _ := openTemp(t, fixedClock(rollupNow))
	minute := rollupNow.Add(-2 * 24 * time.Hour)
	d.Record("reliability", "up", core.Point{At: minute, Value: 4})
	d.Record("reliability", "up", core.Point{At: minute.Add(10 * time.Second), Value: 6})
	mustPrune(t, d)
	d.Record("reliability", "up", core.Point{At: minute.Add(50 * time.Second), Value: 20})

	mustPrune(t, d)

	got := mustMinutes(t, d, "up", minute, minute.Add(time.Minute))
	want := []Minute{{At: minute, Min: 4, Max: 20, Avg: 10}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("merged minute = %+v, want %+v", got, want)
	}
}

func TestAMinuteSplitByThePruneCutoffReadsAsOneMinute(t *testing.T) {
	now := rollupNow.Add(30 * time.Second)
	d, _ := openTemp(t, fixedClock(now))
	minute := rollupNow.Add(-RawRetention)
	for i, value := range []float64{1, 2, 3, 4, 5} {
		d.Record("reliability", "up", core.Point{At: minute.Add(time.Duration(i) * 12 * time.Second), Value: value})
	}

	mustPrune(t, d)

	if raw := countRows(t, d, "reliability", "up"); raw != 2 {
		t.Fatalf("raw points after a mid-minute cutoff = %d, want the 2 after it", raw)
	}
	got := mustMinutes(t, d, "up", minute, now)
	if want := (Minute{At: minute, Min: 1, Max: 5, Avg: 3}); len(got) != 1 || got[0] != want {
		t.Fatalf("split minute = %+v, want one %+v", got, want)
	}
}

func TestQueryReturnsRolledUpAveragesBeforeRawPoints(t *testing.T) {
	d, _ := openTemp(t, fixedClock(rollupNow))
	old := rollupNow.Add(-2 * 24 * time.Hour)
	d.Record("reliability", "up", core.Point{At: old, Value: 2})
	d.Record("reliability", "up", core.Point{At: old.Add(30 * time.Second), Value: 4})
	d.Record("reliability", "up", core.Point{At: rollupNow.Add(-time.Hour), Value: 9})
	mustPrune(t, d)

	points, err := d.Query("reliability", "up", old, rollupNow)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(points) != 2 || points[0].Value != 3 || !points[0].At.Equal(old) || points[1].Value != 9 {
		t.Fatalf("points = %+v, want the old minute's average then the raw point", points)
	}
}

func TestNamesListsASeriesHeldOnlyInRollups(t *testing.T) {
	d, _ := openTemp(t, fixedClock(rollupNow))
	d.Record("reliability", "up", core.Point{At: rollupNow.Add(-2 * 24 * time.Hour), Value: 1})
	d.Record("reliability", "latency_ms", core.Point{At: rollupNow, Value: 1})
	mustPrune(t, d)

	names, err := d.Names("reliability")
	if err != nil {
		t.Fatalf("Names: %v", err)
	}

	if strings.Join(names, ",") != "latency_ms,up" {
		t.Fatalf("Names = %v, want [latency_ms up]", names)
	}
}

func TestAStoreFromBeforeRollupsFoldsItsRawHistoryOnFirstPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	old := rollupNow.Add(-3 * 24 * time.Hour)
	if _, err := legacy.Exec(`
CREATE TABLE points (bucket TEXT NOT NULL, series TEXT NOT NULL, at INTEGER NOT NULL, value REAL NOT NULL);
CREATE INDEX points_bucket_series_at ON points (bucket, series, at);
INSERT INTO points VALUES ('reliability', 'up', ?1, 1), ('reliability', 'up', ?2, 0), ('reliability', 'up', ?3, 1);`,
		old.UnixMilli(), old.Add(30*time.Second).UnixMilli(), rollupNow.UnixMilli()); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}
	d, err := openDisk(path, fixedClock(rollupNow))
	if err != nil {
		t.Fatalf("openDisk on a pre-rollup store: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	mustPrune(t, d)

	got := mustMinutes(t, d, "up", old, rollupNow)
	if len(got) != 2 || got[0] != (Minute{At: old, Min: 0, Max: 1, Avg: 0.5}) || got[1].Avg != 1 {
		t.Fatalf("minutes from a pre-rollup store = %+v, want the folded old minute then the raw one", got)
	}
}

func TestPruningWhileRecordingNeitherLosesNorDoubleCountsAPoint(t *testing.T) {
	const writes = 200
	d, _ := openTemp(t, fixedClock(rollupNow))
	old := rollupNow.Add(-2 * 24 * time.Hour)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range writes {
			d.Record("reliability", "up", core.Point{At: old.Add(time.Duration(i) * time.Second), Value: 1})
		}
	}()
	for pruning := true; pruning; {
		select {
		case <-done:
			pruning = false
		default:
			mustPrune(t, d)
		}
	}

	mustPrune(t, d)

	if folded := countWhere(t, d, "SELECT COALESCE(SUM(samples), 0) FROM rollups"); folded != writes {
		t.Fatalf("rollups hold %d samples after interleaved prunes, want every one of the %d writes exactly once", folded, writes)
	}
}
