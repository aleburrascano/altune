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
