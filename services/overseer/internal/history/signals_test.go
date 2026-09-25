package history

import (
	"altune/overseer/internal/core"
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tick(i int) core.Signal {
	return core.Signal{At: base.Add(time.Duration(i) * time.Second), Kind: "tick", Text: "tick-" + strconv.Itoa(i)}
}

func ticks(from, to int) []core.Signal {
	signals := make([]core.Signal, 0, to-from)
	for i := from; i < to; i++ {
		signals = append(signals, tick(i))
	}
	return signals
}

func loadAll(t *testing.T, d *disk, bucket, ring string) []core.Signal {
	t.Helper()
	signals, err := d.LoadSignals(context.Background(), bucket, ring, 1_000_000)
	if err != nil {
		t.Fatalf("LoadSignals: %v", err)
	}
	return signals
}

func countSignalRows(t *testing.T, d *disk, bucket, ring string) int {
	t.Helper()
	var n int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM signals WHERE bucket = ? AND ring = ?", bucket, ring).Scan(&n); err != nil {
		t.Fatalf("count signal rows: %v", err)
	}
	return n
}

func sameSignals(t *testing.T, label string, got, want []core.Signal) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d signals %+v, want %d %+v", label, len(got), got, len(want), want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.At.Equal(w.At) || g.Kind != w.Kind || g.Text != w.Text || g.CorrID != w.CorrID {
			t.Fatalf("%s[%d] = %+v, want %+v", label, i, g, w)
		}
	}
}

func TestAppendedSignalsLoadBackExactlyOldestFirst(t *testing.T) {
	d, _ := openTemp(t)
	zone := time.FixedZone("CEST", 2*60*60)
	written := []core.Signal{
		{At: base.Add(1500 * time.Microsecond).In(zone), Kind: "health", Text: "db=up redis=up", CorrID: "req-1"},
		{At: base.Add(-time.Hour), Kind: "poll", Text: "down"},
		{},
	}

	d.AppendSignals("reliability", "history", 10, written[:1])
	d.AppendSignals("reliability", "history", 10, written[1:])

	sameSignals(t, "loaded", loadAll(t, d, "reliability", "history"), written)
}

func TestStoredSignalsPerRingNeverExceedItsCapacity(t *testing.T) {
	const capacity = 5
	d, _ := openTemp(t)

	for batch := range 7 {
		d.AppendSignals("heartbeat", "ticks", capacity, ticks(batch*3, batch*3+3))
		if got := countSignalRows(t, d, "heartbeat", "ticks"); got > capacity {
			t.Fatalf("after batch %d the ring holds %d rows, over its capacity %d", batch, got, capacity)
		}
	}

	sameSignals(t, "kept", loadAll(t, d, "heartbeat", "ticks"), ticks(16, 21))
}

func TestOneBatchLargerThanTheCapacityKeepsOnlyItsNewest(t *testing.T) {
	d, _ := openTemp(t)

	d.AppendSignals("heartbeat", "ticks", 3, ticks(0, 10))

	sameSignals(t, "kept", loadAll(t, d, "heartbeat", "ticks"), ticks(7, 10))
}

func TestTrimmingOneRingLeavesItsNeighboursAlone(t *testing.T) {
	d, _ := openTemp(t)
	d.AppendSignals("reliability", "history", 10, ticks(0, 4))

	d.AppendSignals("reliability", "poll", 1, ticks(10, 14))
	d.AppendSignals("security", "history", 2, ticks(20, 22))

	sameSignals(t, "reliability history", loadAll(t, d, "reliability", "history"), ticks(0, 4))
}

func TestLoadReadsNoMoreThanTheLimitAndKeepsTheNewest(t *testing.T) {
	d, _ := openTemp(t)
	d.AppendSignals("heartbeat", "ticks", 100, ticks(0, 10))

	loaded, err := d.LoadSignals(context.Background(), "heartbeat", "ticks", 3)
	if err != nil {
		t.Fatalf("LoadSignals: %v", err)
	}
	sameSignals(t, "loaded", loaded, ticks(7, 10))
}

func TestANegativeLimitLoadsNothingRatherThanEverything(t *testing.T) {
	d, _ := openTemp(t)
	d.AppendSignals("heartbeat", "ticks", 100, ticks(0, 10))

	loaded, err := d.LoadSignals(context.Background(), "heartbeat", "ticks", -1)

	if err != nil || len(loaded) != 0 {
		t.Fatalf("LoadSignals(limit -1) = %d signals, %v; want none", len(loaded), err)
	}
}

func TestARowWithAnUnreadableTimeFailsTheLoadOfItsRing(t *testing.T) {
	d, _ := openTemp(t)
	d.AppendSignals("heartbeat", "ticks", 10, ticks(0, 2))
	if _, err := d.db.Exec("UPDATE signals SET at = 'yesterday-ish' WHERE bucket = 'heartbeat'"); err != nil {
		t.Fatalf("corrupt row: %v", err)
	}

	loaded, err := d.LoadSignals(context.Background(), "heartbeat", "ticks", 10)

	if err == nil {
		t.Fatalf("LoadSignals over a corrupt row = %+v, want an error", loaded)
	}
}

func TestAHistoryFileFromBeforeSignalsGainsTheTableOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open old file: %v", err)
	}
	if _, err := old.Exec(schema); err != nil {
		t.Fatalf("write old schema: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close old file: %v", err)
	}

	d, err := openDisk(path)
	if err != nil {
		t.Fatalf("openDisk over a pre-signals file: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	d.AppendSignals("heartbeat", "ticks", 10, ticks(0, 2))

	sameSignals(t, "loaded", loadAll(t, d, "heartbeat", "ticks"), ticks(0, 2))
}

func TestAFailedSignalWriteIsLoggedOnce(t *testing.T) {
	d, _ := openTemp(t)
	logs := captureLog(t)
	if _, err := d.db.Exec("DROP TABLE signals"); err != nil {
		t.Fatalf("drop signals: %v", err)
	}

	d.AppendSignals("heartbeat", "ticks", 10, ticks(0, 1))
	d.AppendSignals("heartbeat", "ticks", 10, ticks(1, 2))

	if got := strings.Count(logs.String(), "history.write_failed"); got != 1 {
		t.Fatalf("write_failed logged %d times, want once: %s", got, logs.String())
	}
}

func TestAnUnavailableHistoryRestoresNothingAndAcceptsWrites(t *testing.T) {
	var store Database = unavailable{}

	store.AppendSignals("heartbeat", "ticks", 10, ticks(0, 2))
	loaded, err := store.LoadSignals(context.Background(), "heartbeat", "ticks", 10)

	if err != nil || len(loaded) != 0 {
		t.Fatalf("unavailable LoadSignals = %+v, %v; want nothing", loaded, err)
	}
}
