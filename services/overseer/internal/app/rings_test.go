package app_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const eventsRing = "events"

type ringBucket struct {
	*fakeBucket
	ring      *core.RingStore
	firstSeen chan []core.Signal
}

func newRingBucket(capacity int, emit ...core.Signal) *ringBucket {
	b := &ringBucket{ring: core.NewRingStore(capacity), firstSeen: make(chan []core.Signal, 1)}
	b.fakeBucket = &fakeBucket{id: "ringed", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call > 1 {
			return nil, nil
		}
		b.firstSeen <- b.ring.Snapshot()
		return emit, nil
	}, store: func(signals []core.Signal) {
		for _, s := range signals {
			b.ring.Add(s)
		}
	}}
	return b
}

func (b *ringBucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{eventsRing: b.ring}
}

type panickingRingsBucket struct{ *fakeBucket }

func (panickingRingsBucket) Rings() map[string]*core.RingStore { panic("Rings blew up") }

var restartBase = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func event(i int, text string) core.Signal {
	return core.Signal{At: restartBase.Add(time.Duration(i) * time.Second), Kind: "event", Text: text, CorrID: "corr-" + text}
}

func firstSeen(t *testing.T, b *ringBucket) []core.Signal {
	t.Helper()
	select {
	case seen := <-b.firstSeen:
		return seen
	default:
		t.Fatal("the app served before it ran its first collect")
		return nil
	}
}

func bootAndStop(t *testing.T, path string, b *ringBucket, others ...core.Bucket) []core.Signal {
	t.Helper()
	r := boot(t, testConfig(path), append(others, b)...)
	seen := firstSeen(t, b)
	r.halt(t)
	return seen
}

func storedEvents(t *testing.T, path string) []core.Signal {
	t.Helper()
	store := history.Open(path, history.WithSchemaTimeout(time.Minute))
	t.Cleanup(func() { _ = store.Close() })
	signals, err := store.LoadSignals(context.Background(), "ringed", eventsRing, 1_000_000)
	if err != nil {
		t.Fatalf("LoadSignals: %v", err)
	}
	return signals
}

func sameEvents(t *testing.T, label string, got, want []core.Signal) {
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

func historyPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "history.db")
}

func TestARestartOnTheSameHistoryFileRestoresTheRingOldestFirstBeforeTheFirstCollect(t *testing.T) {
	path := historyPath(t)
	written := []core.Signal{event(0, "a"), event(1, "b"), event(2, "c")}
	captureLog(t)
	bootAndStop(t, path, newRingBucket(10, written...))

	seen := bootAndStop(t, path, newRingBucket(10))

	sameEvents(t, "ring at the first collect after restart", seen, written)
}

func TestARestartKeepsOnlyTheNewestCapacitySignals(t *testing.T) {
	path := historyPath(t)
	captureLog(t)
	bootAndStop(t, path, newRingBucket(3, event(0, "a"), event(1, "b"), event(2, "c"), event(3, "d"), event(4, "e")))

	seen := bootAndStop(t, path, newRingBucket(3))

	sameEvents(t, "restored ring", seen, []core.Signal{event(2, "c"), event(3, "d"), event(4, "e")})
}

func TestRestoredSignalsAreNotWrittenBackAsNewRows(t *testing.T) {
	path := historyPath(t)
	captureLog(t)
	bootAndStop(t, path, newRingBucket(10, event(0, "a"), event(1, "b")))

	bootAndStop(t, path, newRingBucket(10, event(5, "z")))

	sameEvents(t, "stored rows after two boots", storedEvents(t, path), []core.Signal{event(0, "a"), event(1, "b"), event(5, "z")})
}

func TestACorruptSignalsTableBootsWithAnEmptyRingAndSaysSo(t *testing.T) {
	path := historyPath(t)
	captureLog(t)
	bootAndStop(t, path, newRingBucket(10, event(0, "a")))
	corruptSignalTimes(t, path)
	logs := captureLog(t)

	seen := bootAndStop(t, path, newRingBucket(10))

	if len(seen) != 0 {
		t.Fatalf("ring over a corrupt signals table = %+v, want empty", seen)
	}
	if !strings.Contains(logs.String(), "overseer.restore.ring_failed") {
		t.Fatalf("log does not say the ring restore failed: %s", logs.String())
	}
}

func TestAnUnreadableHistoryFileBootsWithAnEmptyRing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "history.db")
	captureLog(t)

	seen := bootAndStop(t, path, newRingBucket(10))

	if len(seen) != 0 {
		t.Fatalf("ring over an unopenable history file = %+v, want empty", seen)
	}
}

func openSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func corruptSignalTimes(t *testing.T, path string) {
	t.Helper()
	if _, err := openSQLite(t, path).Exec("UPDATE signals SET at = 'not a time'"); err != nil {
		t.Fatalf("corrupt signals: %v", err)
	}
}

func signalRows(t *testing.T, db *sql.DB) (rows, appended int) {
	t.Helper()
	err := db.QueryRow("SELECT COUNT(*), COALESCE(MAX(seq), 0) FROM signals WHERE bucket = 'ringed'").Scan(&rows, &appended)
	if err != nil {
		t.Fatalf("read signals rows: %v", err)
	}
	return rows, appended
}

func TestStoredRowsPerRingNeverExceedItsCapacityAcrossCycles(t *testing.T) {
	path := historyPath(t)
	captureLog(t)
	cfg := testConfig(path)
	cfg.TickInterval = 5 * time.Millisecond
	b := newRingBucket(4)
	b.collect = func(_ context.Context, call int32) ([]core.Signal, error) {
		return []core.Signal{event(int(call), "e")}, nil
	}

	r := boot(t, cfg, b)
	db := openSQLite(t, path)
	eventually(t, func() bool {
		rows, appended := signalRows(t, db)
		if rows > 4 {
			t.Fatalf("%d rows stored for a ring of capacity 4 while the app ran", rows)
		}
		return appended >= 3*4
	}, "the app did not persist the ring on each of 12 cycles, three times its capacity, while it ran")
	r.halt(t)

	if stored := storedEvents(t, path); len(stored) != 4 {
		t.Fatalf("stored rows = %d; want the ring capacity 4", len(stored))
	}
}

func TestAPanickingRingsHookDoesNotStopTheOtherBucketsRestoring(t *testing.T) {
	path := historyPath(t)
	seed := history.Open(path, history.WithSchemaTimeout(time.Minute))
	seed.AppendSignals("ringed", eventsRing, 10, []core.Signal{event(0, "a")})
	if err := seed.Close(); err != nil {
		t.Fatalf("close seeded history: %v", err)
	}
	logs := captureLog(t)

	seen := bootAndStop(t, path, newRingBucket(10), panickingRingsBucket{plain("a-boom")})

	sameEvents(t, "ring beside a panicking bucket", seen, []core.Signal{event(0, "a")})
	if panics := logs.named("overseer.start.bucket_panic"); len(panics) != 1 || panics[0]["stage"] != "Rings" {
		t.Errorf("start bucket_panic lines = %v; want one at stage Rings", panics)
	}
}

func TestShutdownFlushesSignalsAddedSinceTheLastCycle(t *testing.T) {
	path := historyPath(t)
	captureLog(t)
	b := newRingBucket(10)
	r := boot(t, testConfig(path), b)
	firstSeen(t, b)

	b.ring.Add(event(0, "late"))
	r.halt(t)

	sameEvents(t, "stored after shutdown", storedEvents(t, path), []core.Signal{event(0, "late")})
}
