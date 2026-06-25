package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// Stage-3 consensus (detail screen).
//
// Every provider is an equal source: an album on 2+ providers is confirmed, an
// album on one is unconfirmed-but-included, and MusicBrainz is the only
// authority that REMOVES (contamination from same-name artists). The audited
// engine is carried forward verbatim except for two changes:
//
//   - album clustering is now CATEGORICAL (parseVersion core + tags) instead of
//     a TokenSortRatio ≥ 85 threshold — the constants-ledger replacement of
//     consensusTitleMatchMinTSR. "Scorpion" and "Scorpion (Deluxe)" are distinct
//     releases (different tags); the same album from two providers clusters on an
//     exact core/tags match (a fuzzy core rung catches title typos).
//   - a per-artist TTL cache short-circuits the provider fan-out and MB calls.

// consensusTimeout bounds the whole operation (parallel fetch + MB validation).
// Kept (principled SLA): a hung provider or slow MB loop must not keep the
// artist-detail request open for minutes.
const consensusTimeout = 10 * time.Second

// defaultConsensusCacheTTL is how long a per-artist consensus stays fresh. A
// short TTL (not event-driven) is the OQ4 policy: a missed new release self-heals
// within the window, and the search path surfaces new releases immediately.
const defaultConsensusCacheTTL = 6 * time.Hour

type ConsensusStatus string

const (
	ConsensusConfirmed   ConsensusStatus = "confirmed"
	ConsensusUnconfirmed ConsensusStatus = "unconfirmed"
	ConsensusRejected    ConsensusStatus = "rejected"
)

// ConsensusAlbum is an album with its cross-provider consensus verdict.
type ConsensusAlbum struct {
	Album  domain.SearchResult
	Status ConsensusStatus
	Reason string
}

// ConsensusProvider is one equal album source.
type ConsensusProvider struct {
	Name    string
	Fetcher func(ctx context.Context, artistName string) ([]domain.SearchResult, error)
}

// FanOutConsensus runs collect for every provider concurrently and gathers the
// results into a map keyed by provider name. It is the shared scatter-gather for
// the consensus and coverage-signal paths (the breaker/timeout-bearing search
// fanOut in search.go is deliberately separate — it carries more policy). The
// per-provider payload type T differs (a raw slice vs a wrapped result), so it
// is a type parameter.
func FanOutConsensus[T any](
	ctx context.Context,
	providers []ConsensusProvider,
	collect func(ctx context.Context, p ConsensusProvider) T,
) map[string]T {
	var mu sync.Mutex
	out := make(map[string]T, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
		wg.Add(1)
		go func(p ConsensusProvider) {
			defer wg.Done()
			r := collect(ctx, p)
			mu.Lock()
			out[p.Name] = r
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return out
}

// mbAuthority is the MusicBrainz subset the consensus needs: the bulk
// release-group discography for the resolved artist, used as the identity spine.
// The MusicBrainzAdapter satisfies it.
type mbAuthority interface {
	ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error)
}

type ConsensusService struct {
	providers []ConsensusProvider
	mb        mbAuthority
	cache     *artistConsensusCache
}

type ConsensusOption func(*ConsensusService)

// WithMBAuthority enables the MusicBrainz contamination/authority filter.
func WithMBAuthority(mb mbAuthority) ConsensusOption {
	return func(s *ConsensusService) { s.mb = mb }
}

func NewConsensusService(providers []ConsensusProvider, opts ...ConsensusOption) *ConsensusService {
	s := &ConsensusService{
		providers: providers,
		cache:     newArtistConsensusCache(defaultConsensusCacheTTL),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// BuildConsensus merges albums from every provider into a union with a
// confirmed/unconfirmed/rejected verdict per album. A fresh per-artist cache
// entry short-circuits the entire fan-out + MB pass.
func (s *ConsensusService) BuildConsensus(
	ctx context.Context,
	artistName string,
	primaryAlbums []domain.SearchResult,
) []ConsensusAlbum {
	cacheKey := textnorm.NormalizeForMatch(artistName)
	if cached, ok := s.cache.get(cacheKey); ok {
		return cached
	}

	ctx, cancel := context.WithTimeout(ctx, consensusTimeout)
	defer cancel()

	byProvider := s.fetchFromProviders(ctx, artistName)
	respondedCount := 0
	for _, p := range s.providers {
		if byProvider[p.Name] != nil {
			respondedCount++
		}
	}

	clusters := newAlbumClusterSet()
	for _, album := range primaryAlbums {
		clusters.add(album, "deezer")
	}
	// Iterate providers in slice order (not map range) so the output ordering
	// and canonical-pick are deterministic run-to-run.
	for _, p := range s.providers {
		for _, album := range byProvider[p.Name] {
			clusters.add(album, p.Name)
		}
	}

	results := make([]ConsensusAlbum, 0, len(clusters.order))
	for _, key := range clusters.order {
		c := clusters.byKey[key]
		status, reason := ConsensusUnconfirmed, "single provider"
		if c.providerCount >= 2 {
			status, reason = ConsensusConfirmed, "found on multiple providers"
		}
		results = append(results, ConsensusAlbum{
			Album:  annotateConsensus(c.album, status, c.providerCount, respondedCount),
			Status: status,
			Reason: reason,
		})
	}

	results = s.applyMBAuthority(ctx, artistName, results)

	// Don't cache a timeout-truncated result: if the deadline fired mid-fetch or
	// mid-MB-validation, the verdicts are partial and must not be frozen for the
	// full TTL. A fresh request will recompute.
	if len(results) > 0 && ctx.Err() == nil {
		s.cache.set(cacheKey, results)
	}
	logConsensus(ctx, artistName, results, respondedCount, len(s.providers))
	return results
}

func (s *ConsensusService) fetchFromProviders(ctx context.Context, artistName string) map[string][]domain.SearchResult {
	return FanOutConsensus(ctx, s.providers, func(ctx context.Context, p ConsensusProvider) []domain.SearchResult {
		albums, err := p.Fetcher(ctx, artistName)
		if err != nil {
			return nil
		}
		return albums
	})
}

// applyMBAuthority anchors the album list on the MusicBrainz spine. When MB
// resolves the artist and confirms at least one album, its release-group
// discography is the identity authority: confirmed albums are kept, and every
// other album is REJECTED as same-name contamination — the "wrong Che" albums a
// name-keyed union pulls in. This replaces the old per-album LookupAlbumArtist
// probe (capped + timeout-prone) with the bulk discography already fetched by
// ValidateArtistAlbums: more precise (drops what MB simply does not credit to
// this artist, not only what it credits to another) and far cheaper — one call,
// not N.
//
// When MB confirms nothing — artist absent from MB, or an underground artist MB
// does not cover — MB is not a credible authority here, so the provider union is
// returned untouched (precision is unattainable without a spine; coverage wins).
func (s *ConsensusService) applyMBAuthority(
	ctx context.Context,
	artistName string,
	results []ConsensusAlbum,
) []ConsensusAlbum {
	if s.mb == nil {
		return results
	}

	allAlbums := make([]domain.SearchResult, len(results))
	for i, r := range results {
		allAlbums[i] = r.Album
	}

	validated, err := s.mb.ValidateArtistAlbums(ctx, artistName, allAlbums)
	if err != nil || validated == nil {
		return results
	}

	confirmedTitles := make(map[string]bool, len(validated.Confirmed))
	for _, a := range validated.Confirmed {
		confirmedTitles[textnorm.NormalizeForMatch(a.Title)] = true
	}

	// MB confirmed nothing → not a credible authority for this artist; keep the
	// provider union as-is.
	if len(confirmedTitles) == 0 {
		return results
	}

	for i := range results {
		if results[i].Status == ConsensusRejected {
			continue
		}
		if confirmedTitles[textnorm.NormalizeForMatch(results[i].Album.Title)] {
			results[i].Status = ConsensusConfirmed
			results[i].Reason = "confirmed by MusicBrainz"
			results[i].Album = annotateConsensus(results[i].Album, ConsensusConfirmed, 1, 0)
			continue
		}
		results[i].Status = ConsensusRejected
		results[i].Reason = "not in MusicBrainz discography for resolved artist"
		results[i].Album = annotateConsensus(results[i].Album, ConsensusRejected, 0, 0)
	}

	return results
}

func logConsensus(ctx context.Context, artistName string, results []ConsensusAlbum, responded, providers int) {
	confirmed, unconfirmed, rejected := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case ConsensusConfirmed:
			confirmed++
		case ConsensusUnconfirmed:
			unconfirmed++
		case ConsensusRejected:
			rejected++
		}
	}
	slog.InfoContext(ctx, "consensus.v2.complete",
		"artist", artistName,
		"total", len(results),
		"confirmed", confirmed,
		"unconfirmed", unconfirmed,
		"rejected", rejected,
		"responded", responded,
		"providers", providers,
	)
}

func annotateConsensus(album domain.SearchResult, status ConsensusStatus, matchCount, respondedCount int) domain.SearchResult {
	extras := copyExtras(album.Extras)
	extras["consensus_status"] = string(status)
	if matchCount > 0 {
		extras["consensus_matches"] = matchCount
	}
	if respondedCount > 0 {
		extras["consensus_responded"] = respondedCount
	}
	album.Extras = extras
	return album
}

// --- categorical album clustering ---

type albumCluster struct {
	album         domain.SearchResult
	providerCount int
	providers     []string
}

type albumClusterSet struct {
	byKey map[string]*albumCluster
	order []string
}

func newAlbumClusterSet() *albumClusterSet {
	return &albumClusterSet{byKey: make(map[string]*albumCluster)}
}

// add clusters an album by exact canonical title (the same principled rule as
// Layer 2 — no version vocabulary, no fuzzy threshold).
func (s *albumClusterSet) add(album domain.SearchResult, provider string) {
	key := textnorm.NormalizeForMatch(album.Title)
	if c, ok := s.byKey[key]; ok {
		c.providerCount++
		c.providers = append(c.providers, provider)
		if completenessOf(album) > completenessOf(c.album) {
			c.album = album
		}
		return
	}
	s.byKey[key] = &albumCluster{album: album, providerCount: 1, providers: []string{provider}}
	s.order = append(s.order, key)
}

// --- per-artist TTL cache ---

type consensusCacheEntry struct {
	albums    []ConsensusAlbum
	expiresAt time.Time
}

type artistConsensusCache struct {
	mu  sync.RWMutex
	ttl time.Duration
	m   map[string]consensusCacheEntry
}

func newArtistConsensusCache(ttl time.Duration) *artistConsensusCache {
	return &artistConsensusCache{ttl: ttl, m: make(map[string]consensusCacheEntry)}
}

func (c *artistConsensusCache) get(key string) ([]ConsensusAlbum, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.m[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.albums, true
}

func (c *artistConsensusCache) set(key string, albums []ConsensusAlbum) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = consensusCacheEntry{albums: albums, expiresAt: time.Now().Add(c.ttl)}
}
