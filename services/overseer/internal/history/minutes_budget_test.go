package history

import (
	"altune/overseer/internal/core"
	"testing"
	"time"
)

const sevenDayBudget = 200 * time.Millisecond

var fullStoreSeries = []string{"up", "latency_ms", "rps", "p99_ms"}

func openFullStore(t testing.TB) *disk {
	t.Helper()
	path := t.TempDir() + "/history.db"
	d, err := openDisk(path, fixedClock(rollupNow))
	if err != nil {
		t.Fatalf("openDisk: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	rawStep := RawRetention / DefaultCap
	rollupMinutes := int((DefaultRetention - RawRetention) / time.Minute)
	for _, series := range fullStoreSeries {
		raw := make([]core.Point, 0, DefaultCap)
		for i := range DefaultCap {
			raw = append(raw, core.Point{At: rollupNow.Add(-RawRetention + time.Duration(i)*rawStep), Value: float64(i % 97)})
		}
		seedPoints(t, d, series, raw)
		seedRollups(t, d, series, rollupNow.Add(-DefaultRetention), rollupMinutes)
	}
	return d
}

func seedRollups(t testing.TB, d *disk, series string, from time.Time, minutes int) {
	t.Helper()
	tx, err := d.db.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	for minute := range minutes {
		at := from.Add(time.Duration(minute) * time.Minute).UnixMilli()
		if _, err := tx.Exec("INSERT INTO rollups (bucket, series, minute, lowest, highest, total, samples) VALUES ('reliability', ?, ?, 1, 3, 6, 3)", series, at); err != nil {
			t.Fatalf("seed rollup: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
}

func sevenDayMinutes(t testing.TB, d *disk) []core.Minute {
	t.Helper()
	minutes, err := d.Minutes("reliability", "latency_ms", rollupNow.Add(-DefaultRetention), rollupNow)
	if err != nil {
		t.Fatalf("Minutes: %v", err)
	}
	return minutes
}

func BenchmarkSevenDayMinutesOnAFullStoreStayUnderBudget(b *testing.B) {
	d := openFullStore(b)
	var minutes []core.Minute
	for b.Loop() {
		minutes = sevenDayMinutes(b, d)
	}
	if want := int(DefaultRetention / time.Minute); len(minutes) != want {
		b.Fatalf("7d minutes = %d, want one per minute (%d)", len(minutes), want)
	}
	if perRead := b.Elapsed() / time.Duration(b.N); perRead > sevenDayBudget {
		b.Fatalf("7d read on a full store took %v, over the %v budget", perRead, sevenDayBudget)
	}
}
