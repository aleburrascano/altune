package enrich

import (
	"context"
	"errors"
	"testing"
)

// memStringCache is the string-typed in-memory NameKeyedCache used to test
// CachedLookup itself (the enricher tests each carry their own typed twin).
type memStringCache struct {
	pos  map[string]string
	neg  map[string]bool
	sets int
	negs int
}

func newMemStringCache() *memStringCache {
	return &memStringCache{pos: map[string]string{}, neg: map[string]bool{}}
}
func (c *memStringCache) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := c.pos[k]
	return v, ok, nil
}
func (c *memStringCache) Set(_ context.Context, k string, v string) error {
	c.sets++
	c.pos[k] = v
	return nil
}
func (c *memStringCache) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}
func (c *memStringCache) SetNegative(_ context.Context, k string) error {
	c.negs++
	c.neg[k] = true
	return nil
}

// countingFetch returns a fetch func that records how often it ran.
func countingFetch(calls *int, value string, found bool, err error) func(context.Context) (string, bool, error) {
	return func(context.Context) (string, bool, error) {
		*calls++
		return value, found, err
	}
}

func TestCachedLookup_PositiveHitSkipsLoader(t *testing.T) {
	cache := newMemStringCache()
	cache.pos["daft punk"] = "cached"
	calls := 0

	got, err := CachedLookup(context.Background(), cache, "daft punk", "", countingFetch(&calls, "fresh", true, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "cached" {
		t.Errorf("want the cached value, got %q", got)
	}
	if calls != 0 {
		t.Errorf("a positive hit must not call fetch, got %d calls", calls)
	}
}

func TestCachedLookup_NegativeHitReturnsEmptyWithoutLoader(t *testing.T) {
	cache := newMemStringCache()
	cache.neg["nobody"] = true
	calls := 0

	got, err := CachedLookup(context.Background(), cache, "nobody", "", countingFetch(&calls, "fresh", true, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("want empty on a negative hit, got %q", got)
	}
	if calls != 0 {
		t.Errorf("a negative hit must not call fetch, got %d calls", calls)
	}
}

func TestCachedLookup_LoaderHitIsPositiveCached(t *testing.T) {
	cache := newMemStringCache()
	calls := 0
	fetch := countingFetch(&calls, "fresh", true, nil)

	got, err := CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fresh" || calls != 1 || cache.sets != 1 || cache.negs != 0 {
		t.Fatalf("first call: got=%q calls=%d sets=%d negs=%d", got, calls, cache.sets, cache.negs)
	}

	// Second call is served from the cache.
	got, _ = CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if got != "fresh" || calls != 1 {
		t.Errorf("second call must hit the cache, got=%q calls=%d", got, calls)
	}
}

func TestCachedLookup_DefinitiveMissIsNegativeCached(t *testing.T) {
	cache := newMemStringCache()
	calls := 0
	fetch := countingFetch(&calls, "", false, nil)

	got, err := CachedLookup(context.Background(), cache, "nobody", "", fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" || cache.negs != 1 || cache.sets != 0 {
		t.Fatalf("miss: got=%q negs=%d sets=%d", got, cache.negs, cache.sets)
	}

	// Second call is answered by the negative cache, not the loader.
	_, _ = CachedLookup(context.Background(), cache, "nobody", "", fetch)
	if calls != 1 {
		t.Errorf("negative-cached miss must not re-fetch, got %d calls", calls)
	}
}

func TestCachedLookup_TransientErrorDegradesAndIsNotCached(t *testing.T) {
	cache := newMemStringCache()
	calls := 0
	fetch := countingFetch(&calls, "", false, errors.New("network down"))

	got, err := CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if err != nil {
		t.Fatalf("transient errors are swallowed (best-effort), got %v", err)
	}
	if got != "" {
		t.Errorf("want empty on transient error, got %q", got)
	}
	if cache.sets != 0 || cache.negs != 0 {
		t.Errorf("a transient error must not poison the cache, got sets=%d negs=%d", cache.sets, cache.negs)
	}

	// The next call retries the loader — nothing was cached.
	_, _ = CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if calls != 2 {
		t.Errorf("want a retry after a transient error, got %d calls", calls)
	}
}

func TestCachedLookup_NilCacheRunsUncached(t *testing.T) {
	calls := 0
	fetch := countingFetch(&calls, "fresh", true, nil)

	got, err := CachedLookup[string](context.Background(), nil, "daft punk", "", fetch)
	if err != nil || got != "fresh" {
		t.Fatalf("nil cache: got=%q err=%v", got, err)
	}
	_, _ = CachedLookup[string](context.Background(), nil, "daft punk", "", fetch)
	if calls != 2 {
		t.Errorf("nil cache must fetch every call, got %d calls", calls)
	}
}
