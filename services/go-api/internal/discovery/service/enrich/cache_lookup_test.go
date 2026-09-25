package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

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
	if !errors.Is(err, ErrDegraded) {
		t.Fatalf("a transient error must surface as ErrDegraded, got %v", err)
	}
	if got != "" {
		t.Errorf("want empty on transient error, got %q", got)
	}
	if cache.sets != 0 || cache.negs != 0 {
		t.Errorf("a transient error must not poison the cache, got sets=%d negs=%d", cache.sets, cache.negs)
	}

	_, _ = CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if calls != 2 {
		t.Errorf("want a retry after a transient error, got %d calls", calls)
	}
}

func TestCachedLookup_UnkeyableNameRunsUncached(t *testing.T) {
	cache := newMemStringCache()
	nameKey := kindNameKey(domain.ResultKindTrack, "!!!", "!!!")
	cache.pos[nameKey] = "another symbol-only track's data"
	calls := 0

	got, err := CachedLookup(context.Background(), cache, nameKey, "", countingFetch(&calls, "", false, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("a name that normalizes away must not be served a shared entry, got %q", got)
	}
	if calls != 1 {
		t.Errorf("want the fetch to run uncached, got %d calls", calls)
	}
	if cache.sets != 0 || cache.negs != 0 {
		t.Errorf("a name that normalizes away must not be written, got sets=%d negs=%d", cache.sets, cache.negs)
	}
}

func TestKindNameKey_DistinguishesSplitsOfTheSameWords(t *testing.T) {
	artistSplit := kindNameKey(domain.ResultKindTrack, "A B", "C")
	titleSplit := kindNameKey(domain.ResultKindTrack, "A", "B C")
	if artistSplit == titleSplit {
		t.Errorf("distinct artist/title splits must not share a key, both = %q", artistSplit)
	}
}

func TestKindNameKey_PartitionsByKind(t *testing.T) {
	track := kindNameKey(domain.ResultKindTrack, "Daft Punk", "One More Time")
	album := kindNameKey(domain.ResultKindAlbum, "Daft Punk", "One More Time")
	if track == album {
		t.Errorf("distinct kinds must not share a key, both = %q", track)
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

type failingNegativeCache struct{ memStringCache }

func (failingNegativeCache) GetNegative(context.Context, string) (bool, error) {
	return false, errors.New("redis unreachable")
}

func TestCachedLookup_ErroringGetNegativeStillFallsThroughToFetch(t *testing.T) {
	cache := &failingNegativeCache{*newMemStringCache()}
	calls := 0

	got, err := CachedLookup(context.Background(), cache, "daft punk", "", countingFetch(&calls, "fresh", true, nil))
	if err != nil {
		t.Fatalf("lookup failed on a cache error: %v", err)
	}
	if got != "fresh" || calls != 1 {
		t.Errorf("got %q after %d fetches, want fresh after 1", got, calls)
	}
}
