package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type inMemoryConsensusCache struct{ m map[string][]ConsensusAlbum }

func newInMemoryConsensusCache() *inMemoryConsensusCache {
	return &inMemoryConsensusCache{m: make(map[string][]ConsensusAlbum)}
}

func (c *inMemoryConsensusCache) Get(_ context.Context, key string) ([]ConsensusAlbum, bool, error) {
	v, ok := c.m[key]
	return v, ok, nil
}

func (c *inMemoryConsensusCache) Set(_ context.Context, key string, v []ConsensusAlbum) error {
	c.m[key] = v
	return nil
}

func (c *inMemoryConsensusCache) GetNegative(context.Context, string) (bool, error) {
	return false, nil
}
func (c *inMemoryConsensusCache) SetNegative(context.Context, string) error { return nil }

var _ ports.NameKeyedCache[[]ConsensusAlbum] = (*inMemoryConsensusCache)(nil)

func consensusProvider(name string, albums ...string) ConsensusProvider {
	return ConsensusProvider{
		Name: name,
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			out := make([]domain.SearchResult, len(albums))
			for i, title := range albums {
				out[i] = domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, Subtitle: "Artist"}
			}
			return out, nil
		},
	}
}

func statusByTitle(albums []ConsensusAlbum) map[string]ConsensusStatus {
	m := make(map[string]ConsensusStatus, len(albums))
	for _, a := range albums {
		m[a.Album.Title] = a.Status
	}
	return m
}

type fakeMB struct {
	mbid        string
	confirmed   []string
	validateErr error
}

func (m *fakeMB) ResolveArtistIdentity(_ context.Context, _ string) (*ports.ArtistIdentity, error) {
	if m.mbid == "" {
		return nil, nil
	}
	return &ports.ArtistIdentity{MBID: m.mbid}, nil
}

func (m *fakeMB) ValidateArtistAlbums(_ context.Context, _ string, _ []domain.SearchResult) (*ports.AlbumValidationResult, error) {
	if m.validateErr != nil {
		return nil, m.validateErr
	}
	conf := make([]domain.SearchResult, len(m.confirmed))
	for i, t := range m.confirmed {
		conf[i] = domain.SearchResult{Title: t}
	}
	return &ports.AlbumValidationResult{Confirmed: conf}, nil
}

func TestConsensus_ConfirmedAndUnconfirmed(t *testing.T) {
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Album A", "Album B"),
		consensusProvider("musicbrainz", "Album A"),
	})
	got := svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	byTitle := statusByTitle(got)
	if byTitle["Album A"] != ConsensusConfirmed {
		t.Errorf("Album A (2 providers) = %v, want confirmed", byTitle["Album A"])
	}
	if byTitle["Album B"] != ConsensusUnconfirmed {
		t.Errorf("Album B (1 provider) = %v, want unconfirmed", byTitle["Album B"])
	}
}

func TestConsensus_DistinctAlbumsStaySeparate(t *testing.T) {
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Scorpion", "Scorpion (Deluxe)", "Take Care"),
		consensusProvider("musicbrainz", "Scorpion"),
	})
	got := svc.BuildConsensus(context.Background(), "Drake", domain.ProviderDeezer, "", nil)

	byTitle := statusByTitle(got)
	if _, ok := byTitle["Take Care"]; !ok {
		t.Error("expected the distinct album 'Take Care' to remain")
	}
	if byTitle["Scorpion"] != ConsensusConfirmed {
		t.Errorf("Scorpion = %v, want confirmed (deluxe folds in)", byTitle["Scorpion"])
	}
	if _, ok := byTitle["Scorpion (Deluxe)"]; ok {
		t.Error("'Scorpion (Deluxe)' should have folded into 'Scorpion', not stand alone")
	}
}

func TestConsensus_CacheSkipsProviderCalls(t *testing.T) {
	var calls int32
	p := ConsensusProvider{
		Name: "lastfm",
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			atomic.AddInt32(&calls, 1)
			return []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "X", Subtitle: "Artist"}}, nil
		},
	}
	svc := NewConsensusService([]ConsensusProvider{p}, WithConsensusCache(newInMemoryConsensusCache()))

	svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)
	svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("provider fetched %d times, want 1 (second served from cache)", n)
	}
}

func TestConsensus_CacheKeyedBySeedIdentity(t *testing.T) {
	var calls int32
	p := ConsensusProvider{
		Name: "lastfm",
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			atomic.AddInt32(&calls, 1)
			return []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "X", Subtitle: "Che"}}, nil
		},
	}
	cache := newInMemoryConsensusCache()
	svc := NewConsensusService([]ConsensusProvider{p}, WithConsensusCache(cache))

	svc.BuildConsensus(context.Background(), "Che", domain.ProviderDeezer, "rapper-1", nil)
	svc.BuildConsensus(context.Background(), "Che", domain.ProviderDeezer, "soul-2", nil)
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("provider fetched %d times, want 2 (distinct seeds must not share a cache entry)", n)
	}
	if len(cache.m) != 2 {
		t.Errorf("cache entries = %d, want 2 distinct keys", len(cache.m))
	}

	svc.BuildConsensus(context.Background(), "Che", domain.ProviderDeezer, "rapper-1", nil)
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("provider fetched %d times, want 2 (same seed re-served from cache)", n)
	}
}

func TestConsensus_MBErrorServedButNotCached(t *testing.T) {
	var calls int32
	p := ConsensusProvider{
		Name: "lastfm",
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			atomic.AddInt32(&calls, 1)
			return []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "X", Subtitle: "Artist"}}, nil
		},
	}
	cache := newInMemoryConsensusCache()
	mb := &fakeMB{mbid: "mb1", validateErr: context.DeadlineExceeded}
	svc := NewConsensusService([]ConsensusProvider{p}, WithMBAuthority(mb), WithConsensusCache(cache))

	got := svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1 (union still served on MB error)", len(got))
	}
	if len(cache.m) != 0 {
		t.Errorf("cache entries = %d, want 0 (MB error must skip the cache write)", len(cache.m))
	}
	svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("provider fetched %d times, want 2 (nothing cached after MB error)", n)
	}
}

func TestConsensus_SameProviderVotesCountOnce(t *testing.T) {
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("itunes", "Album X"),
	})
	primary := []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "Album X", Subtitle: "Artist"}}

	got := svc.BuildConsensus(context.Background(), "Artist", domain.ProviderITunes, "i-1", primary)

	if s := statusByTitle(got)["Album X"]; s != ConsensusUnconfirmed {
		t.Errorf("Album X = %v, want unconfirmed (iTunes primary + iTunes by-name is ONE provider)", s)
	}
}

func TestConsensus_DeterministicAcrossRuns(t *testing.T) {
	build := func() []ConsensusAlbum {
		svc := NewConsensusService([]ConsensusProvider{
			consensusProvider("lastfm", "A", "B", "C"),
			consensusProvider("musicbrainz", "B", "D"),
			consensusProvider("itunes", "A", "C", "E"),
		})
		return svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)
	}

	first := build()
	for i := 0; i < 5; i++ {
		got := build()
		if len(got) != len(first) {
			t.Fatalf("run %d: len = %d, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].Album.Title != first[j].Album.Title || got[j].Status != first[j].Status {
				t.Fatalf("run %d position %d: got (%q,%v), want (%q,%v)",
					i, j, got[j].Album.Title, got[j].Status, first[j].Album.Title, first[j].Status)
			}
		}
	}
}

func TestConsensus_MBSpineRejectsAlbumsNotInDiscography(t *testing.T) {
	mb := &fakeMB{mbid: "mb1", confirmed: []string{"Real Album"}}
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Real Album", "Other Artist Album"),
	}, WithMBAuthority(mb))

	byTitle := statusByTitle(svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil))

	if byTitle["Real Album"] != ConsensusConfirmed {
		t.Errorf("Real Album = %v, want confirmed", byTitle["Real Album"])
	}
	if byTitle["Other Artist Album"] != ConsensusRejected {
		t.Errorf("Other Artist Album = %v, want rejected (not in MB discography)", byTitle["Other Artist Album"])
	}
}

func TestConsensus_MBNotCredibleKeepsUnion(t *testing.T) {
	mb := &fakeMB{mbid: "mb1", confirmed: nil}
	svc := NewConsensusService([]ConsensusProvider{
		consensusProvider("lastfm", "Album A", "Album B"),
		consensusProvider("itunes", "Album A"),
	}, WithMBAuthority(mb))

	byTitle := statusByTitle(svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil))

	if byTitle["Album A"] != ConsensusConfirmed {
		t.Errorf("Album A (2 providers) = %v, want confirmed", byTitle["Album A"])
	}
	if byTitle["Album B"] != ConsensusUnconfirmed {
		t.Errorf("Album B (1 provider) = %v, want unconfirmed (kept, MB not credible)", byTitle["Album B"])
	}
}

func failingConsensusProvider(calls *atomic.Int32) ConsensusProvider {
	return ConsensusProvider{
		Name:     "lastfm",
		Provider: domain.ProviderLastFM,
		Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			calls.Add(1)
			return nil, fmt.Errorf("get https://x/?api_key=sekrit: %w", context.DeadlineExceeded)
		},
	}
}

func TestConsensusOpensBreakerAndStopsCallingFailingProvider(t *testing.T) {
	var calls atomic.Int32
	breaker := NewCircuitBreaker()
	svc := NewConsensusService(
		[]ConsensusProvider{failingConsensusProvider(&calls), consensusProvider("itunes", "A")},
		WithConsensusCircuitBreaker(breaker),
	)

	for range failureThreshold + 3 {
		svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)
	}

	if got := calls.Load(); got != failureThreshold {
		t.Errorf("failing provider called %d times, want %d before the circuit opens", got, failureThreshold)
	}
	if breaker.AllowRequest(domain.ProviderLastFM) {
		t.Error("circuit for the failing provider should be open")
	}
}

func TestConsensusLogsFailingProviderByNameWithoutSecrets(t *testing.T) {
	buf := captureLogs(t)
	var calls atomic.Int32
	svc := NewConsensusService([]ConsensusProvider{failingConsensusProvider(&calls)})

	svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	out := buf.String()
	if !strings.Contains(out, "consensus.provider_failed") || !strings.Contains(out, `"provider":"lastfm"`) {
		t.Errorf("expected a provider_failed log naming lastfm, got %s", out)
	}
	if strings.Contains(out, "sekrit") {
		t.Errorf("log leaked a secret: %s", out)
	}
}

func chronoAlbum(title string, year int) ConsensusAlbum {
	album := res(domain.ResultKindAlbum, title, "Artist", domain.ProviderDeezer, nil)
	album.Year = year
	return ConsensusAlbum{
		Album:  album,
		Status: ConsensusConfirmed,
	}
}

func TestSortChronological_NewestFirstUnknownLast(t *testing.T) {
	in := []ConsensusAlbum{
		chronoAlbum("Old", 2018),
		chronoAlbum("Unknown", 0),
		chronoAlbum("Newest", 2024),
		chronoAlbum("Mid", 2021),
		chronoAlbum("Older", 2017),
	}

	sortByReleaseDateDesc(in, consensusAlbumSortKey)

	got := make([]string, len(in))
	for i, a := range in {
		got[i] = a.Album.Title
	}
	want := []string{"Newest", "Mid", "Old", "Older", "Unknown"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

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

// Regression test for #568: a panic in a goroutine spawned around a provider
// or port call must be contained, not terminate the process.
func TestFanOutConsensus_PanickingCollectIsContained(t *testing.T) {
	providers := []ConsensusProvider{{Name: "boom"}, {Name: "ok"}}
	out := FanOutConsensus(context.Background(), providers, func(_ context.Context, p ConsensusProvider) int {
		if p.Name == "boom" {
			panic("collect exploded")
		}
		return 7
	})

	if out["ok"] != 7 {
		t.Errorf("ok = %d, want 7", out["ok"])
	}
	if _, present := out["boom"]; present {
		t.Errorf("panicking provider should be absent, got %v", out["boom"])
	}
}
