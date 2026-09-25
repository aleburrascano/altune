package enrich

import (
	"context"
	"errors"
	"testing"
)

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
