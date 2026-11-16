package sqlite_test

import (
	"altune/overseer/internal/history/internal/adapters/sqlite"
	"testing"
	"time"
)

func TestOverCapTrimByMoreThanOneBatchStillTrimsToTheCap(t *testing.T) {
	const rowCap = 50
	const excess = sqlite.PruneBatch + 200
	const total = rowCap + excess
	d, _ := openTemp(t, sqlite.WithCap(rowCap), fixedClock(rollupNow))
	seedRollups(t, d, "up", rollupNow.Add(-time.Duration(total)*time.Minute), total)

	mustPrune(t, d)

	if got := countWhere(t, d, "SELECT COUNT(*) FROM rollups WHERE bucket = 'reliability' AND series = 'up'"); got != rowCap {
		t.Fatalf("rollup rows after trimming %d over-cap rows by more than one batch = %d, want exactly the cap %d", total, got, rowCap)
	}
}
