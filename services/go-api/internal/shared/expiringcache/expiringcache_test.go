package expiringcache_test

import (
	"altune/go-api/internal/shared/expiringcache"
	"strconv"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0)} }

func present[V comparable](t *testing.T, m *expiringcache.Memo[string, V], key string, want V) {
	t.Helper()
	if got, ok := m.Get(key); !ok || got != want {
		t.Errorf("Get(%q) = %v, %v; want %v, true", key, got, ok, want)
	}
}

func absent[V any](t *testing.T, m *expiringcache.Memo[string, V], key string) {
	t.Helper()
	if got, ok := m.Get(key); ok {
		t.Errorf("Get(%q) = %v, true; want a miss", key, got)
	}
}

func TestMemo_UnknownKeyIsNotServed(t *testing.T) {
	m := expiringcache.New[string, int](time.Hour, 4, newClock().now)
	m.Put("humble", 1)

	absent(t, m, "dna")
}

func TestMemo_KeysAreNotLimitedToStrings(t *testing.T) {
	type key struct {
		artist string
		page   int
	}
	m := expiringcache.New[key, string](time.Hour, 4, newClock().now)
	m.Put(key{"kendrick", 1}, "first")

	if got, ok := m.Get(key{"kendrick", 1}); !ok || got != "first" {
		t.Errorf("Get = %q, %v; want first, true", got, ok)
	}
	if _, ok := m.Get(key{"kendrick", 2}); ok {
		t.Error("a different struct key was served")
	}
}

func TestMemo_CapBoundsDistinctKeysToTheNewest(t *testing.T) {
	const capacity = 4
	m := expiringcache.New[string, int](time.Hour, capacity, newClock().now)
	for i := range capacity * 3 {
		m.Put("artist-"+strconv.Itoa(i), i)
	}
	for i := range capacity * 2 {
		absent(t, m, "artist-"+strconv.Itoa(i))
	}
	for i := capacity * 2; i < capacity*3; i++ {
		present(t, m, "artist-"+strconv.Itoa(i), i)
	}
}

func TestMemo_FullOfLiveEntriesEvictsTheLeastRecentlyUsed(t *testing.T) {
	m := expiringcache.New[string, string](time.Hour, 2, newClock().now)
	m.Put("a", "A")
	m.Put("b", "B")
	present(t, m, "a", "A")
	m.Put("c", "C")
	absent(t, m, "b")
	present(t, m, "a", "A")
	present(t, m, "c", "C")
}

func TestMemo_ACapBelowOneHoldsOneEntry(t *testing.T) {
	m := expiringcache.New[string, int](time.Hour, 0, newClock().now)
	m.Put("a", 1)
	present(t, m, "a", 1)
	m.Put("b", 2)
	absent(t, m, "a")
	present(t, m, "b", 2)
}

func TestMemo_ReputReplacesTheValueWithoutTakingAnotherSlot(t *testing.T) {
	m := expiringcache.New[string, int](time.Hour, 2, newClock().now)
	for i := range 10 {
		m.Put("same", i)
	}
	m.Put("x", 100)
	present(t, m, "same", 9)
	m.Put("y", 200)
	absent(t, m, "x")
	present(t, m, "same", 9)
	present(t, m, "y", 200)
}

func TestMemo_ReputRenewsTheTTL(t *testing.T) {
	c := newClock()
	m := expiringcache.New[string, int](time.Hour, 2, c.now)
	m.Put("a", 1)
	c.advance(50 * time.Minute)
	m.Put("a", 2)
	c.advance(50 * time.Minute)
	present(t, m, "a", 2)
}

func TestMemo_AnEntryExpiresAfterItsTTLAndCanBeStoredAgain(t *testing.T) {
	c := newClock()
	m := expiringcache.New[string, int](time.Hour, 50, c.now)
	m.Put("a", 1)
	c.advance(time.Hour)
	present(t, m, "a", 1)
	c.advance(time.Nanosecond)
	absent(t, m, "a")
	m.Put("a", 2)
	present(t, m, "a", 2)
}

func TestMemo_AFullMemoPurgesExpiredEntriesBeforeEvictingLiveOnes(t *testing.T) {
	c := newClock()
	m := expiringcache.New[string, string](time.Hour, 2, c.now)
	m.Put("expiring", "E")
	c.advance(30 * time.Minute)
	m.Put("live", "L")
	c.advance(time.Minute)
	present(t, m, "expiring", "E")
	c.advance(45 * time.Minute)
	m.Put("new", "N")
	present(t, m, "live", "L")
	present(t, m, "new", "N")
}

func TestMemo_AFullMemoSweepsAtMostOncePerTTL(t *testing.T) {
	c := newClock()
	m := expiringcache.New[string, string](time.Hour, 2, c.now)
	c.advance(10 * time.Minute)
	m.Put("x", "X")
	c.advance(40 * time.Minute)
	m.Put("y", "Y")
	c.advance(5 * time.Minute)
	present(t, m, "x", "X")
	c.advance(5 * time.Minute)
	m.Put("z", "Z")
	absent(t, m, "y")
	present(t, m, "x", "X")
	c.advance(15 * time.Minute)
	m.Put("w", "W")
	absent(t, m, "z")
	present(t, m, "w", "W")
}

func TestMemo_ReadingAnExpiredEntryFreesItsSlot(t *testing.T) {
	c := newClock()
	m := expiringcache.New[string, string](time.Hour, 2, c.now)
	c.advance(10 * time.Minute)
	m.Put("a", "A")
	c.advance(50 * time.Minute)
	m.Put("b", "B")
	present(t, m, "a", "A")
	m.Put("c", "C")
	present(t, m, "a", "A")

	c.advance(20 * time.Minute)
	absent(t, m, "a")
	m.Put("d", "D")

	present(t, m, "c", "C")
	present(t, m, "d", "D")
}

func TestMemo_StatsCountEveryGetAsAHitOrAMiss(t *testing.T) {
	c := newClock()
	m := expiringcache.New[string, int](time.Hour, 4, c.now)
	absent(t, m, "a")
	m.Put("a", 1)
	present(t, m, "a", 1)
	present(t, m, "a", 1)
	c.advance(2 * time.Hour)
	absent(t, m, "a")

	if got, want := m.Stats(), (expiringcache.Stats{Hits: 2, Misses: 2}); got != want {
		t.Errorf("Stats = %+v; want %+v", got, want)
	}
}

func TestMemo_PeekServesWithoutCounting(t *testing.T) {
	m := expiringcache.New[string, int](time.Hour, 4, newClock().now)
	m.Put("a", 1)

	if got, ok := m.Peek("a"); !ok || got != 1 {
		t.Errorf("Peek(a) = %v, %v; want 1, true", got, ok)
	}
	if _, ok := m.Peek("b"); ok {
		t.Error("Peek served an unknown key")
	}
	if got := m.Stats(); got != (expiringcache.Stats{}) {
		t.Errorf("Stats = %+v; want zero after Peek only", got)
	}
}
