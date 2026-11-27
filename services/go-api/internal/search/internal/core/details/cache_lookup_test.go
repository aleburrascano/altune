package details_test

import (
	"altune/go-api/internal/search/internal/core/details"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"testing"
)

func countingFetch(calls *int, value string, found bool, err error) func(context.Context) (string, bool, error) {
	return func(context.Context) (string, bool, error) {
		*calls++
		return value, found, err
	}
}

func TestCachedLookup_PositiveHitSkipsLoader(t *testing.T) {
	cache := searchtest.NewNameKeyedCache[string]()
	cache.Seed("daft punk", "cached")
	calls := 0

	got, err := details.CachedLookup(context.Background(), cache, "daft punk", "", countingFetch(&calls, "fresh", true, nil))
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
	cache := searchtest.NewNameKeyedCache[string]()
	cache.SeedNegative("nobody")
	calls := 0

	got, err := details.CachedLookup(context.Background(), cache, "nobody", "", countingFetch(&calls, "fresh", true, nil))
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
	cache := searchtest.NewNameKeyedCache[string]()
	calls := 0
	fetch := countingFetch(&calls, "fresh", true, nil)

	got, err := details.CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fresh" || calls != 1 || cache.Sets() != 1 || cache.Negatives() != 0 {
		t.Fatalf("first call: got=%q calls=%d sets=%d negs=%d", got, calls, cache.Sets(), cache.Negatives())
	}

	got, _ = details.CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if got != "fresh" || calls != 1 {
		t.Errorf("second call must hit the cache, got=%q calls=%d", got, calls)
	}
}

func TestCachedLookup_DefinitiveMissIsNegativeCached(t *testing.T) {
	cache := searchtest.NewNameKeyedCache[string]()
	calls := 0
	fetch := countingFetch(&calls, "", false, nil)

	got, err := details.CachedLookup(context.Background(), cache, "nobody", "", fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" || cache.Negatives() != 1 || cache.Sets() != 0 {
		t.Fatalf("miss: got=%q negs=%d sets=%d", got, cache.Negatives(), cache.Sets())
	}

	_, _ = details.CachedLookup(context.Background(), cache, "nobody", "", fetch)
	if calls != 1 {
		t.Errorf("negative-cached miss must not re-fetch, got %d calls", calls)
	}
}

func TestCachedLookup_TransientErrorDegradesAndIsNotCached(t *testing.T) {
	cache := searchtest.NewNameKeyedCache[string]()
	calls := 0
	fetch := countingFetch(&calls, "", false, errors.New("network down"))

	got, err := details.CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if !errors.Is(err, details.ErrDegraded) {
		t.Fatalf("a transient error must surface as ErrDegraded, got %v", err)
	}
	if got != "" {
		t.Errorf("want empty on transient error, got %q", got)
	}
	if cache.Sets() != 0 || cache.Negatives() != 0 {
		t.Errorf("a transient error must not poison the cache, got sets=%d negs=%d", cache.Sets(), cache.Negatives())
	}

	_, _ = details.CachedLookup(context.Background(), cache, "daft punk", "", fetch)
	if calls != 2 {
		t.Errorf("want a retry after a transient error, got %d calls", calls)
	}
}

func TestCachedLookup_EmptyNameKeyRunsUncached(t *testing.T) {
	cache := searchtest.NewNameKeyedCache[string]()
	cache.Seed("", "another symbol-only track's data")
	calls := 0

	got, err := details.CachedLookup(context.Background(), cache, "", "", countingFetch(&calls, "", false, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("an empty name key must not be served a shared entry, got %q", got)
	}
	if calls != 1 {
		t.Errorf("want the fetch to run uncached, got %d calls", calls)
	}
	if cache.Sets() != 0 || cache.Negatives() != 0 {
		t.Errorf("an empty name key must not be written, got sets=%d negs=%d", cache.Sets(), cache.Negatives())
	}
}

func TestCachedLookup_AnAbsentCacheRunsUncached(t *testing.T) {
	calls := 0
	fetch := countingFetch(&calls, "fresh", true, nil)

	got, err := details.CachedLookup[string](context.Background(), details.NoNameKeyedCache[string]{}, "daft punk", "", fetch)
	if err != nil || got != "fresh" {
		t.Fatalf("absent cache: got=%q err=%v", got, err)
	}
	_, _ = details.CachedLookup[string](context.Background(), details.NoNameKeyedCache[string]{}, "daft punk", "", fetch)
	if calls != 2 {
		t.Errorf("absent cache must fetch every call, got %d calls", calls)
	}
}

type failingNegativeCache struct {
	*searchtest.NameKeyedCache[string]
}

func (failingNegativeCache) GetNegative(context.Context, string) (bool, error) {
	return false, errors.New("redis unreachable")
}

func TestCachedLookup_ErroringGetNegativeStillFallsThroughToFetch(t *testing.T) {
	cache := &failingNegativeCache{searchtest.NewNameKeyedCache[string]()}
	calls := 0

	got, err := details.CachedLookup(context.Background(), cache, "daft punk", "", countingFetch(&calls, "fresh", true, nil))
	if err != nil {
		t.Fatalf("lookup failed on a cache error: %v", err)
	}
	if got != "fresh" || calls != 1 {
		t.Errorf("got %q after %d fetches, want fresh after 1", got, calls)
	}
}

type failingSetCache struct {
	*searchtest.NameKeyedCache[string]
}

func (failingSetCache) Set(context.Context, string, string) error {
	return errors.New("redis read-only")
}

func (failingSetCache) SetNegative(context.Context, string) error {
	return errors.New("redis read-only")
}

func TestCachedLookup_AFailedCacheWriteStillAnswersAndIsLogged(t *testing.T) {
	cases := []struct {
		name    string
		found   bool
		wantMsg string
	}{
		{"positive write", true, "enrichment.cache_set_failed"},
		{"negative write", false, "enrichment.cache_set_negative_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ring := logging.CaptureRing(t, "info")
			cache := &failingSetCache{searchtest.NewNameKeyedCache[string]()}
			calls := 0

			if _, err := details.CachedLookup(context.Background(), cache, "daft punk", "", countingFetch(&calls, "fresh", tc.found, nil)); err != nil {
				t.Fatalf("a cache write error failed the lookup: %v", err)
			}

			for _, r := range ring.Snapshot() {
				if r.Message == tc.wantMsg {
					return
				}
			}
			t.Errorf("no %s line after a failed cache write", tc.wantMsg)
		})
	}
}
