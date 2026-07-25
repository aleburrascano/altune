package service

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

const consensusTimeout = 10 * time.Second

const DefaultConsensusCacheTTL = 6 * time.Hour

type ConsensusStatus string

const (
	ConsensusConfirmed   ConsensusStatus = "confirmed"
	ConsensusUnconfirmed ConsensusStatus = "unconfirmed"
	ConsensusRejected    ConsensusStatus = "rejected"
)

type ConsensusAlbum struct {
	Album  domain.SearchResult
	Status ConsensusStatus
	Reason string
}

type ConsensusProvider struct {
	Name    string
	Fetcher func(ctx context.Context, artistName string) ([]domain.SearchResult, error)
}

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

type mbAuthority interface {
	ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error)
}

type ConsensusService struct {
	providers []ConsensusProvider
	mb        mbAuthority
	cache     ports.NameKeyedCache[[]ConsensusAlbum]
}

type ConsensusOption func(*ConsensusService)

func WithMBAuthority(mb mbAuthority) ConsensusOption {
	return func(s *ConsensusService) { s.mb = mb }
}

func WithConsensusCache(cache ports.NameKeyedCache[[]ConsensusAlbum]) ConsensusOption {
	return func(s *ConsensusService) { s.cache = cache }
}

func NewConsensusService(providers []ConsensusProvider, opts ...ConsensusOption) *ConsensusService {
	s := &ConsensusService{
		providers: providers,
		cache:     noopConsensusCache{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ConsensusService) BuildConsensus(
	ctx context.Context,
	artistName string,
	seedProvider domain.ProviderName,
	seedID string,
	primaryAlbums []domain.SearchResult,
) []ConsensusAlbum {
	cacheKey := textnorm.NormalizeForMatch(artistName)
	if seedID != "" {
		cacheKey += "|" + seedProvider.String() + ":" + seedID
	}
	if cached, ok, err := s.cache.Get(ctx, cacheKey); err == nil && ok {
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
		clusters.add(album, seedProvider.String())
	}
	for _, p := range s.providers {
		for _, album := range byProvider[p.Name] {
			clusters.add(album, p.Name)
		}
	}

	results := make([]ConsensusAlbum, 0, len(clusters.order))
	for _, key := range clusters.order {
		c := clusters.byKey[key]
		status, reason := ConsensusUnconfirmed, "single provider"
		if len(c.providers) >= 2 {
			status, reason = ConsensusConfirmed, "found on multiple providers"
		}
		results = append(results, ConsensusAlbum{
			Album:  annotateConsensus(c.album, status, len(c.providers), respondedCount),
			Status: status,
			Reason: reason,
		})
	}

	results, mbErred := s.applyMBAuthority(ctx, artistName, results)
	sortChronological(results)

	if len(results) > 0 && ctx.Err() == nil && !mbErred {
		_ = s.cache.Set(ctx, cacheKey, results)
	}
	logConsensus(ctx, artistName, results, respondedCount, len(s.providers))
	return results
}

func sortChronological(results []ConsensusAlbum) {
	sort.SliceStable(results, func(i, j int) bool {
		ki, kj := albumReleaseSortKey(results[i].Album), albumReleaseSortKey(results[j].Album)
		if ki == "" || kj == "" {
			return ki != "" && kj == ""
		}
		return ki > kj
	})
}

func (s *ConsensusService) NameGroups(ctx context.Context, artistName string) [][]domain.SearchResult {
	byProvider := s.fetchFromProviders(ctx, artistName)
	groups := make([][]domain.SearchResult, 0, len(s.providers))
	for _, p := range s.providers {
		if albums := byProvider[p.Name]; len(albums) > 0 {
			groups = append(groups, albums)
		}
	}
	return groups
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

func (s *ConsensusService) applyMBAuthority(
	ctx context.Context,
	artistName string,
	results []ConsensusAlbum,
) ([]ConsensusAlbum, bool) {
	if s.mb == nil {
		return results, false
	}

	allAlbums := make([]domain.SearchResult, len(results))
	for i, r := range results {
		allAlbums[i] = r.Album
	}

	validated, err := s.mb.ValidateArtistAlbums(ctx, artistName, allAlbums)
	if err != nil {
		return results, true
	}
	if validated == nil {
		return results, false
	}

	confirmedTitles := make(map[string]bool, len(validated.Confirmed))
	for _, a := range validated.Confirmed {
		confirmedTitles[textnorm.NormalizeForMatch(a.Title)] = true
	}

	if len(confirmedTitles) == 0 {
		return results, false
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

	return results, false
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

type albumCluster struct {
	album     domain.SearchResult
	providers map[string]bool
}

type albumClusterSet struct {
	byKey map[string]*albumCluster
	order []string
}

func newAlbumClusterSet() *albumClusterSet {
	return &albumClusterSet{byKey: make(map[string]*albumCluster)}
}

func (s *albumClusterSet) add(album domain.SearchResult, provider string) {
	key := textnorm.NormalizeForMatch(album.Title)
	if c, ok := s.byKey[key]; ok {
		c.providers[provider] = true
		if completenessOf(album) > completenessOf(c.album) {
			c.album = album
		}
		return
	}
	s.byKey[key] = &albumCluster{album: album, providers: map[string]bool{provider: true}}
	s.order = append(s.order, key)
}

type noopConsensusCache struct{}

func (noopConsensusCache) Get(context.Context, string) ([]ConsensusAlbum, bool, error) {
	return nil, false, nil
}
func (noopConsensusCache) Set(context.Context, string, []ConsensusAlbum) error { return nil }
func (noopConsensusCache) GetNegative(context.Context, string) (bool, error)   { return false, nil }
func (noopConsensusCache) SetNegative(context.Context, string) error           { return nil }
