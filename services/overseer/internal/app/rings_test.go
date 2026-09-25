package app

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const eventsRing = "events"

type ringBucket struct {
	id        string
	ring      *core.RingStore
	emit      []core.Signal
	firstSeen chan []core.Signal
	once      sync.Once
}

func newRingBucket(capacity int, emit ...core.Signal) *ringBucket {
	return &ringBucket{id: "ringed", ring: core.NewRingStore(capacity), emit: emit, firstSeen: make(chan []core.Signal, 1)}
}

func (b *ringBucket) Meta() core.Meta { return core.Meta{ID: b.id} }

func (b *ringBucket) Collect(context.Context) ([]core.Signal, error) {
	var emitted []core.Signal
	b.once.Do(func() {
		b.firstSeen <- b.ring.Snapshot()
		emitted = b.emit
	})
	return emitted, nil
}

func (b *ringBucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.ring.Add(s)
	}
}

func (b *ringBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

func (b *ringBucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{eventsRing: b.ring}
}

type panickingRingsBucket struct{ stubBucket }

func (panickingRingsBucket) Rings() map[string]*core.RingStore { panic("Rings blew up") }

var restartBase = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func event(i int, text string) core.Signal {
	return core.Signal{At: restartBase.Add(time.Duration(i) * time.Second), Kind: "event", Text: text, CorrID: "corr-" + text}
}

func bootAndStop(t *testing.T, path string, b *ringBucket) []core.Signal {
	t.Helper()
	reg := core.NewRegistry()
	reg.Register(b)
	a := newApp(historyConfig(path), reg)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()

	var seen []core.Signal
	select {
	case seen = <-b.firstSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("the app never ran its first collect")
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	return seen
}

func storedEvents(t *testing.T, path string) []core.Signal {
	t.Helper()
	store := history.Open(path)
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

func TestARestartOnTheSameHistoryFileRestoresTheRingOldestFirstBeforeTheFirstCollect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	written := []core.Signal{event(0, "a"), event(1, "b"), event(2, "c")}
	captureSlog(t)
	bootAndStop(t, path, newRingBucket(10, written...))

	seen := bootAndStop(t, path, newRingBucket(10))

	sameEvents(t, "ring at the first collect after restart", seen, written)
}

func TestARestartKeepsOnlyTheNewestCapacitySignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	captureSlog(t)
	bootAndStop(t, path, newRingBucket(3, event(0, "a"), event(1, "b"), event(2, "c"), event(3, "d"), event(4, "e")))

	seen := bootAndStop(t, path, newRingBucket(3))

	sameEvents(t, "restored ring", seen, []core.Signal{event(2, "c"), event(3, "d"), event(4, "e")})
}

func TestRestoredSignalsAreNotWrittenBackAsNewRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	written := []core.Signal{event(0, "a"), event(1, "b")}
	captureSlog(t)
	bootAndStop(t, path, newRingBucket(10, written...))

	bootAndStop(t, path, newRingBucket(10, event(5, "z")))

	sameEvents(t, "stored rows after two boots", storedEvents(t, path), []core.Signal{event(0, "a"), event(1, "b"), event(5, "z")})
}

func TestACorruptSignalsTableBootsWithAnEmptyRingAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	captureSlog(t)
	bootAndStop(t, path, newRingBucket(10, event(0, "a")))
	corruptSignalTimes(t, path)
	logs := captureSlog(t)

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
	captureSlog(t)

	seen := bootAndStop(t, path, newRingBucket(10))

	if len(seen) != 0 {
		t.Fatalf("ring over an unopenable history file = %+v, want empty", seen)
	}
}

func corruptSignalTimes(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open for corruption: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE signals SET at = 'not a time'"); err != nil {
		t.Fatalf("corrupt signals: %v", err)
	}
}

func TestStoredRowsPerRingNeverExceedItsCapacityAcrossPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	store := history.Open(path)
	t.Cleanup(func() { _ = store.Close() })
	b := newRingBucket(4)
	reg := core.NewRegistry()
	reg.Register(b)
	journal := &ringJournal{log: store}
	journal.restore(context.Background(), reg)

	for i := range 25 {
		b.Store([]core.Signal{event(i, "e")})
		journal.persist()
	}

	stored, err := store.LoadSignals(context.Background(), "ringed", eventsRing, 1_000_000)
	if err != nil || len(stored) != 4 {
		t.Fatalf("stored rows = %d, %v; want the ring capacity 4", len(stored), err)
	}
}

func TestAPanickingRingsHookDoesNotStopTheOtherBucketsRestoring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	store := history.Open(path)
	t.Cleanup(func() { _ = store.Close() })
	store.AppendSignals("ringed", eventsRing, 10, []core.Signal{event(0, "a")})
	b := newRingBucket(10)
	reg := core.NewRegistry()
	reg.Register(panickingRingsBucket{stubBucket{id: "a-boom"}})
	reg.Register(b)
	captureSlog(t)

	(&ringJournal{log: store}).restore(context.Background(), reg)

	sameEvents(t, "ring beside a panicking bucket", b.ring.Snapshot(), []core.Signal{event(0, "a")})
}

func TestShutdownFlushesSignalsAddedSinceTheLastCycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	store := history.Open(path)
	b := newRingBucket(10)
	reg := core.NewRegistry()
	reg.Register(b)
	a := newTestApp(reg, time.Second, time.Second)
	a.history = store
	a.rings = ringJournal{log: store}
	ctx, cancel := context.WithCancel(context.Background())
	a.rings.restore(ctx, reg)
	b.Store([]core.Signal{event(0, "late")})

	a.releaseHistory(cancel, a.startPruner(ctx))

	sameEvents(t, "stored after shutdown", storedEvents(t, path), []core.Signal{event(0, "late")})
}
