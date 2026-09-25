package history

import (
	"altune/overseer/internal/core"
	"context"
	"testing"
	"time"
)

func TestOverCapTrimByMoreThanOneBatchStillTrimsToTheCap(t *testing.T) {
	const rowCap = 50
	const excess = pruneBatch + 200
	const total = rowCap + excess
	d, _ := openTemp(t, WithCap(rowCap), fixedClock(rollupNow))
	seedRollups(t, d, "up", rollupNow.Add(-time.Duration(total)*time.Minute), total)

	mustPrune(t, d)

	if got := countWhere(t, d, "SELECT COUNT(*) FROM rollups WHERE bucket = 'reliability' AND series = 'up'"); got != rowCap {
		t.Fatalf("rollup rows after trimming %d over-cap rows by more than one batch = %d, want exactly the cap %d", total, got, rowCap)
	}
}

func TestOverCapTrimCapsEachDeleteAtOneBatchAndLetsRecordInBetween(t *testing.T) {
	const rowCap = 50
	const excess = pruneBatch + 200
	const total = rowCap + excess
	d, _ := openTemp(t, WithCap(rowCap), fixedClock(rollupNow))
	seedRollups(t, d, "up", rollupNow.Add(-time.Duration(total)*time.Minute), total)
	key := seriesKey{bucket: "reliability", series: "up"}

	trimmed, err := d.trimBatch(context.Background(), trimRollupsBatchSQL, key)
	if err != nil {
		t.Fatalf("trimBatch: %v", err)
	}
	if trimmed != pruneBatch {
		t.Fatalf("first trimBatch deleted %d rows, want exactly the batch size %d", trimmed, pruneBatch)
	}
	if got := countWhere(t, d, "SELECT COUNT(*) FROM rollups WHERE bucket = 'reliability' AND series = 'up'"); got != total-pruneBatch {
		t.Fatalf("rows left after one batch = %d, want %d (still over the cap, so a second batch is required)", got, total-pruneBatch)
	}

	d.Record("reliability", "latency_ms", core.Point{At: rollupNow, Value: 1})
	if got := countRows(t, d, "reliability", "latency_ms"); got != 1 {
		t.Fatalf("Record between over-cap batches = %d rows, want 1 (the connection must be free between batches)", got)
	}

	if err := d.trimSeriesOverCap(context.Background(), trimRollupsBatchSQL, key); err != nil {
		t.Fatalf("trimSeriesOverCap: %v", err)
	}
	if got := countWhere(t, d, "SELECT COUNT(*) FROM rollups WHERE bucket = 'reliability' AND series = 'up'"); got != rowCap {
		t.Fatalf("rows after finishing the remaining batches = %d, want exactly the cap %d", got, rowCap)
	}
}
