package expiringcache_test

import (
	"altune/go-api/internal/search/internal/core/expiringcache"
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }

func assertServed(t *testing.T, memo *expiringcache.Memo[string, int], key string, want int) {
	t.Helper()
	got, memoized := memo.Get(key)
	if !memoized || got != want {
		t.Errorf("Get(%q) = (%d, %v), want (%d, true)", key, got, memoized, want)
	}
}

func assertNotServed(t *testing.T, memo *expiringcache.Memo[string, int], key string) {
	t.Helper()
	if got, memoized := memo.Get(key); memoized || got != 0 {
		t.Errorf("Get(%q) = (%d, %v), want (0, false)", key, got, memoized)
	}
}

func TestMemo_UnknownKeyIsNotServed(t *testing.T) {
	memo := expiringcache.New[string, int](time.Hour, 4, newClock().Now)
	memo.Put("humble", 1)

	assertNotServed(t, memo, "dna")
}

func TestMemo_ServesAnEntryUntilItsTTLRunsOutOnTheInjectedClock(t *testing.T) {
	clock := newClock()
	memo := expiringcache.New[string, int](time.Hour, 4, clock.Now)
	memo.Put("humble", 1)

	clock.Advance(time.Hour)
	assertServed(t, memo, "humble", 1)

	clock.Advance(time.Nanosecond)
	assertNotServed(t, memo, "humble")
}

func TestMemo_PutReplacesTheValueAndRestartsItsTTL(t *testing.T) {
	clock := newClock()
	memo := expiringcache.New[string, int](time.Hour, 4, clock.Now)
	memo.Put("humble", 1)

	clock.Advance(30 * time.Minute)
	memo.Put("humble", 2)
	clock.Advance(45 * time.Minute)

	assertServed(t, memo, "humble", 2)
}

func TestMemo_FullOfLiveEntriesDropsNewKeysAndKeepsTheOldOnes(t *testing.T) {
	memo := expiringcache.New[string, int](time.Hour, 2, newClock().Now)
	memo.Put("humble", 1)
	memo.Put("dna", 2)

	memo.Put("loyalty", 3)

	assertNotServed(t, memo, "loyalty")
	assertServed(t, memo, "humble", 1)
	assertServed(t, memo, "dna", 2)
}

func TestMemo_FullMemoEvictsOnlyExpiredEntriesToMakeRoom(t *testing.T) {
	clock := newClock()
	memo := expiringcache.New[string, int](time.Hour, 2, clock.Now)
	memo.Put("humble", 1)
	clock.Advance(30 * time.Minute)
	memo.Put("dna", 2)
	clock.Advance(45 * time.Minute)

	memo.Put("loyalty", 3)

	assertServed(t, memo, "loyalty", 3)
	assertServed(t, memo, "dna", 2)
	assertNotServed(t, memo, "humble")
}
