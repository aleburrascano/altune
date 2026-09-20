package service

import (
	"altune/go-api/internal/discovery/domain"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// logRecordsFor returns every captured record logged under the event msg.
func logRecordsFor(t *testing.T, buf *bytes.Buffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

func TestConsensus_NameGroups(t *testing.T) {
	svc := NewConsensusService([]ConsensusProvider{
		{Name: "broken", Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			return nil, errors.New("down")
		}},
		consensusProvider("lastfm", "Album A", "Album B"),
		{Name: "empty", Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			return nil, nil
		}},
		consensusProvider("itunes", "Album C"),
	})

	groups := svc.NameGroups(context.Background(), "Artist")

	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (erroring + empty providers dropped)", len(groups))
	}
	if len(groups[0]) != 2 || groups[0][0].Title != "Album A" {
		t.Errorf("groups[0] = %v, want lastfm's two albums first (slice order)", titles(groups[0]))
	}
	if len(groups[1]) != 1 || groups[1][0].Title != "Album C" {
		t.Errorf("groups[1] = %v, want itunes' album", titles(groups[1]))
	}
}

func TestConsensus_RespondedCountsCleanEmptyButNotErrors(t *testing.T) {
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Album A"),
		{Name: "no-match", Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			return nil, nil
		}},
		{Name: "broken", Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			return nil, errors.New("down")
		}},
	})

	results := svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	got := results[0].Album.Extras["consensus_responded"]
	if got != 2 {
		t.Errorf("consensus_responded = %v, want 2 (clean empty responded, erroring did not)", got)
	}
}

// Issue #2239: a provider answering 500 or 429 fails fast, leaving the
// deadline intact, so the partial union used to be frozen for every user for
// DefaultConsensusCacheTTL.
func TestConsensus_ProviderFailureLeavesAnswerUncachedAndSaysSo(t *testing.T) {
	cache := newInMemoryConsensusCache()
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Album A"),
		{Name: "broken", Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			return nil, errors.New("429 too many requests")
		}},
	}, WithConsensusCache(cache))
	buf := captureProductionLogs(t)

	got := svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	if len(got) != 1 {
		t.Fatalf("results = %d, want the reachable provider's album still served", len(got))
	}
	if len(cache.m) != 0 {
		t.Errorf("cache entries = %d, want 0 (a partial answer must not be cached for %v)", len(cache.m), DefaultConsensusCacheTTL)
	}
	recs := logRecordsFor(t, buf, "consensus.partial_not_cached")
	if len(recs) != 1 {
		t.Fatalf("got %d consensus.partial_not_cached records, want 1:\n%s", len(recs), buf)
	}
	if recs[0]["responded"] != float64(1) || recs[0]["providers"] != float64(2) {
		t.Errorf("record = %v, want responded=1 of providers=2", recs[0])
	}
}

func TestConsensus_EveryProviderRespondedIsCached(t *testing.T) {
	cache := newInMemoryConsensusCache()
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Album A"),
		consensusProvider("itunes", "Album A"),
	}, WithConsensusCache(cache))
	buf := captureProductionLogs(t)

	svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	if len(cache.m) != 1 {
		t.Errorf("cache entries = %d, want 1 (a complete answer is cacheable)", len(cache.m))
	}
	if recs := logRecordsFor(t, buf, "consensus.partial_not_cached"); len(recs) != 0 {
		t.Errorf("got %d consensus.partial_not_cached records for a complete answer, want none:\n%s", len(recs), buf)
	}
}

func TestConsensus_TimeoutTruncatedNeverCached(t *testing.T) {
	blocked := ConsensusProvider{
		Name: "slow",
		Fetcher: func(ctx context.Context, _ string) ([]domain.SearchResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	cache := newInMemoryConsensusCache()
	svc := NewConsensusService([]ConsensusProvider{blocked}, WithConsensusCache(cache))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	primary := []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "Seed Album", Subtitle: "Artist"}}
	got := svc.BuildConsensus(ctx, "Artist", domain.ProviderDeezer, "d1", primary)

	if len(got) != 1 {
		t.Fatalf("results = %d, want the seed album served despite the timeout", len(got))
	}
	if len(cache.m) != 0 {
		t.Errorf("cache entries = %d, want 0 (timeout-truncated result must not be cached)", len(cache.m))
	}
}

func TestConsensus_NameOnlyKeyWhenNoSeedID(t *testing.T) {
	var calls int
	p := ConsensusProvider{
		Name: "lastfm",
		Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			calls++
			return []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "X", Subtitle: "Che"}}, nil
		},
	}
	cache := newInMemoryConsensusCache()
	svc := NewConsensusService([]ConsensusProvider{p}, WithConsensusCache(cache))

	svc.BuildConsensus(context.Background(), "Che", domain.ProviderDeezer, "seed-1", nil)
	svc.BuildConsensus(context.Background(), "Che", domain.ProviderDeezer, "", nil)
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (seeded and seedless keys are distinct)", calls)
	}
	if _, ok := cache.m["che"]; !ok {
		t.Errorf("cache keys = %v, want the name-only key \"che\" for the seedless call", mapKeys(cache.m))
	}
	if _, ok := cache.m["che|deezer:seed-1"]; !ok {
		t.Errorf("cache keys = %v, want the seed-scoped key", mapKeys(cache.m))
	}

	svc.BuildConsensus(context.Background(), "Che", domain.ProviderDeezer, "", nil)
	if calls != 2 {
		t.Errorf("provider calls = %d, want 2 (name-only key re-served from cache)", calls)
	}
}

func mapKeys(m map[string][]ConsensusAlbum) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNoopConsensusCache_NegativesAreInert(t *testing.T) {
	var c noopConsensusCache
	if hit, err := c.GetNegative(context.Background(), "k"); hit || err != nil {
		t.Errorf("GetNegative = %v/%v, want false/nil", hit, err)
	}
	if err := c.SetNegative(context.Background(), "k"); err != nil {
		t.Errorf("SetNegative = %v, want nil", err)
	}
}

func TestFanOutConsensus_CollectsEveryProvider(t *testing.T) {
	providers := []ConsensusProvider{
		consensusProvider("a", "A1"),
		consensusProvider("b", "B1", "B2"),
	}
	out := FanOutConsensus(context.Background(), providers, func(ctx context.Context, p ConsensusProvider) int {
		albums, _ := p.Fetcher(ctx, "x")
		return len(albums)
	})
	if out["a"] != 1 || out["b"] != 2 {
		t.Errorf("collected = %v, want a=1 b=2", out)
	}
}
