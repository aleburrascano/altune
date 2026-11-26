package core_test

import (
	"altune/overseer/internal/core"
	"sync"
	"testing"
	"time"
)

func TestRingStoreStaysBounded(t *testing.T) {
	const capacity = 8
	s := core.NewRingStore(capacity)

	for i := 0; i < capacity*10; i++ {
		s.Add(core.Signal{At: time.Unix(int64(i), 0), Kind: "tick"})
	}

	if got := s.Len(); got != capacity {
		t.Fatalf("Len = %d, want capped at %d", got, capacity)
	}
	if got := s.Cap(); got != capacity {
		t.Fatalf("Cap = %d, want %d", got, capacity)
	}
	if got := len(s.Snapshot()); got != capacity {
		t.Fatalf("Snapshot len = %d, want %d", got, capacity)
	}
}

func TestRingStoreRetainsNewest(t *testing.T) {
	const capacity = 3
	s := core.NewRingStore(capacity)
	for i := 0; i < 5; i++ {
		s.Add(core.Signal{At: time.Unix(int64(i), 0)})
	}

	snap := s.Snapshot()
	want := []int64{2, 3, 4}
	if len(snap) != len(want) {
		t.Fatalf("snapshot len = %d, want %d", len(snap), len(want))
	}
	for i, w := range want {
		if snap[i].At.Unix() != w {
			t.Errorf("snapshot[%d] = %d, want %d", i, snap[i].At.Unix(), w)
		}
	}
}

func TestRingStoreDropsNothingUntilFull(t *testing.T) {
	const capacity = 4
	s := core.NewRingStore(capacity)

	for i := 0; i < capacity; i++ {
		s.Add(core.Signal{At: time.Unix(int64(i), 0)})
	}

	if got := s.Dropped(); got != 0 {
		t.Fatalf("Dropped after filling exactly to cap = %d, want 0", got)
	}
}

func TestRingStoreCountsEachOverwriteOnceFull(t *testing.T) {
	const capacity = 4
	s := core.NewRingStore(capacity)

	for i := 0; i < capacity*3; i++ {
		s.Add(core.Signal{At: time.Unix(int64(i), 0)})
	}

	if got := s.Dropped(); got != capacity*2 {
		t.Fatalf("Dropped after %d adds into a %d ring = %d, want %d", capacity*3, capacity, got, capacity*2)
	}
}

func TestRingStoreDroppedIsRaceFreeUnderConcurrentAddAndRead(t *testing.T) {
	const capacity = 8
	const adds = 10000
	s := core.NewRingStore(capacity)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < adds; i++ {
			s.Add(core.Signal{At: time.Unix(int64(i), 0)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < adds; i++ {
			_ = s.Dropped()
		}
	}()
	wg.Wait()

	if got := s.Dropped(); got != adds-capacity {
		t.Fatalf("Dropped = %d, want %d (every add past capacity counted once)", got, adds-capacity)
	}
}

func TestRingStoreClampsCapacity(t *testing.T) {
	for _, c := range []int{0, -5} {
		s := core.NewRingStore(c)
		s.Add(core.Signal{})
		s.Add(core.Signal{})
		if s.Cap() != 1 || s.Len() != 1 {
			t.Fatalf("capacity %d: Cap=%d Len=%d, want 1/1", c, s.Cap(), s.Len())
		}
	}
}

func numbered(from, to int) []core.Signal {
	signals := make([]core.Signal, 0, to-from)
	for i := from; i < to; i++ {
		signals = append(signals, core.Signal{At: time.Unix(int64(i), 0), Kind: "tick"})
	}
	return signals
}

func seconds(signals []core.Signal) []int64 {
	out := make([]int64, 0, len(signals))
	for _, s := range signals {
		out = append(out, s.At.Unix())
	}
	return out
}

func sameSeconds(t *testing.T, label string, got []core.Signal, want []int64) {
	t.Helper()
	have := seconds(got)
	if len(have) != len(want) {
		t.Fatalf("%s = %v, want %v", label, have, want)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, have, want)
		}
	}
}

func TestRestoreKeepsTheNewestCapacitySignalsOldestFirstWithNoDroppedCount(t *testing.T) {
	ring := core.NewRingStore(3)

	ring.Restore(numbered(0, 5))

	sameSeconds(t, "restored snapshot", ring.Snapshot(), []int64{2, 3, 4})
	if ring.Dropped() != 0 {
		t.Fatalf("Dropped after restore = %d, want 0", ring.Dropped())
	}
}

func TestRestoreReplacesWhatTheRingHeldBefore(t *testing.T) {
	ring := core.NewRingStore(4)
	for _, s := range numbered(100, 107) {
		ring.Add(s)
	}

	ring.Restore(numbered(0, 2))

	sameSeconds(t, "snapshot after restore over a full ring", ring.Snapshot(), []int64{0, 1})
	if ring.Dropped() != 0 {
		t.Fatalf("Dropped after restore = %d, want the earlier evictions cleared", ring.Dropped())
	}
}

func TestAddAfterAFullRestoreEvictsTheOldestRestoredSignal(t *testing.T) {
	ring := core.NewRingStore(3)
	ring.Restore(numbered(0, 3))

	ring.Add(core.Signal{At: time.Unix(9, 0)})

	sameSeconds(t, "snapshot", ring.Snapshot(), []int64{1, 2, 9})
}

func TestAddAfterAPartialRestoreAppendsBehindTheRestoredSignals(t *testing.T) {
	ring := core.NewRingStore(4)
	ring.Restore(numbered(0, 2))

	ring.Add(core.Signal{At: time.Unix(9, 0)})

	sameSeconds(t, "snapshot", ring.Snapshot(), []int64{0, 1, 9})
}

func TestAddedSinceTheRestoreReportsOnlySignalsAddedAfterIt(t *testing.T) {
	ring := core.NewRingStore(5)
	ring.Restore(numbered(0, 3))
	mark := ring.Len()

	ring.Add(core.Signal{At: time.Unix(7, 0)})
	ring.Add(core.Signal{At: time.Unix(8, 0)})
	fresh, added := ring.AddedSince(mark)

	sameSeconds(t, "fresh since restore", fresh, []int64{7, 8})
	if added != 5 {
		t.Fatalf("added = %d, want 5", added)
	}
}

func TestAddedSinceWithNothingNewReportsNothing(t *testing.T) {
	ring := core.NewRingStore(3)
	for _, s := range numbered(0, 7) {
		ring.Add(s)
	}
	_, added := ring.AddedSince(0)

	fresh, again := ring.AddedSince(added)

	if len(fresh) != 0 || again != added {
		t.Fatalf("AddedSince(%d) = %v, %d; want nothing new and the same mark", added, seconds(fresh), again)
	}
}

func TestAddedSinceAfterMoreAddsThanCapacityReportsOnlyWhatTheRingStillHolds(t *testing.T) {
	ring := core.NewRingStore(3)
	ring.Add(core.Signal{At: time.Unix(0, 0)})
	_, mark := ring.AddedSince(0)

	for _, s := range numbered(1, 9) {
		ring.Add(s)
	}
	fresh, added := ring.AddedSince(mark)

	sameSeconds(t, "fresh after overflow", fresh, []int64{6, 7, 8})
	if added != 9 {
		t.Fatalf("added = %d, want 9", added)
	}
}
