package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

func TestConsensus_NameGroups(t *testing.T) {
	// One provider errors, one returns nothing, two return albums: NameGroups
	// keeps only the responding providers' groups, in provider-slice order.
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

func TestConsensus_TimeoutTruncatedNeverCached(t *testing.T) {
	// The provider blocks past the caller's deadline: the fan-out returns
	// truncated (ctx.Err() != nil), and the partial verdicts must NOT be frozen
	// in the cache for the TTL.
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
	// Without a seed id the cache key falls back to the bare normalized name —
	// the documented same-name-collision risk: a seedless call after a seeded one
	// misses the seeded entry and recomputes under its own name-only key.
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

	// The seedless call now hits its own name-only entry.
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
