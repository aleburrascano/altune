package service

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type GetArtistContentService struct {
	providers     map[domain.ProviderName]ports.ArtistContentProvider
	consensus     *ConsensusService
	identityStore ports.IdentityStore
	mbAnchor      ports.MBDiscographyAnchor
}

func NewGetArtistContentService(
	providers map[domain.ProviderName]ports.ArtistContentProvider,
	opts ...ArtistContentOption,
) *GetArtistContentService {
	s := &GetArtistContentService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type ArtistContentOption func(*GetArtistContentService)

func WithConsensusService(c *ConsensusService) ArtistContentOption {
	return func(s *GetArtistContentService) { s.consensus = c }
}

func WithContentIdentityStore(store ports.IdentityStore) ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityStore = store }
}

func WithMBAnchor(anchor ports.MBDiscographyAnchor) ArtistContentOption {
	return func(s *GetArtistContentService) { s.mbAnchor = anchor }
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityStore != nil {
		identity, _ := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		if tracks := s.v2TopTracks(ctx, identity, artistName); len(tracks) > 0 {
			return okContentResponse(providerName, tracks, limit), nil
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return errorContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "artist_top_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistTopTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return okContentResponse(providerName, results, limit), nil
}

type identityContentFetch func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)

var detailFanOutTimeout = consensusTimeout

func (s *GetArtistContentService) fanOutByIdentity(ctx context.Context, identity ResolvedArtistIdentity, artistName string, fetch identityContentFetch) [][]domain.SearchResult {
	ctx, cancel := context.WithTimeout(ctx, detailFanOutTimeout)
	defer cancel()

	type job struct {
		provider domain.ProviderName
		p        ports.ArtistContentProvider
		id       string
	}
	var jobs []job
	for _, name := range orderedProviderNames(s.providers) {
		p := s.providers[name]
		id := providerContentID(identity, name)
		if id == "" {
			id = resolveArtistIDByName(ctx, p, artistName)
		}
		if id == "" {
			continue
		}
		jobs = append(jobs, job{provider: name, p: p, id: id})
	}

	groups := make([][]domain.SearchResult, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			res, err := fetch(ctx, j.p, j.provider, j.id)
			if err != nil {
				slog.DebugContext(ctx, "artist_content.fanout.provider_failed",
					"provider", j.provider.String(), "error", err)
				return
			}
			groups[i] = res
		}(i, j)
	}
	wg.Wait()

	out := make([][]domain.SearchResult, 0, len(groups))
	for _, g := range groups {
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityStore != nil {
		identity, _ := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		if albums := s.v2Albums(ctx, identity, artistName); len(albums) > 0 {
			return okContentResponse(providerName, albums, limit), nil
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return errorContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "artist_albums.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistAlbums(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}

	results = dedupAlbums(results)

	if artistName != "" && s.consensus != nil {
		consensusResults := s.consensus.BuildConsensus(ctx, artistName, providerName, externalID, results)
		var kept []domain.SearchResult
		for _, cr := range consensusResults {
			if cr.Status != ConsensusRejected {
				kept = append(kept, cr.Album)
			}
		}
		if kept == nil {
			kept = []domain.SearchResult{}
		}
		results = kept
	}

	normalizeAlbumYears(results)
	sortAlbumsByReleaseDateDesc(results)

	return okContentResponse(providerName, results, limit), nil
}

func dedupAlbums(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]int)
	var deduped []domain.SearchResult

	for _, r := range results {
		normTitle := textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
		if idx, ok := seen[normTitle]; ok {
			if r.TrackCount > deduped[idx].TrackCount {
				deduped[idx] = r
			}
			continue
		}
		seen[normTitle] = len(deduped)
		deduped = append(deduped, r)
	}
	return deduped
}

func sortAlbumsByReleaseDateDesc(results []domain.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		ki, kj := albumReleaseSortKey(results[i]), albumReleaseSortKey(results[j])
		if ki == "" || kj == "" {
			return ki != "" && kj == ""
		}
		return ki > kj
	})
}

func albumReleaseSortKey(r domain.SearchResult) string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year)
	}
	return ""
}

func normalizeAlbumYears(results []domain.SearchResult) {
	for i := range results {
		if results[i].Year != 0 || len(results[i].ReleaseDate) < 4 {
			continue
		}
		if y := parseYear(results[i].ReleaseDate[:4]); y > 0 {
			results[i].Year = y
		}
	}
}

func parseYear(s string) int {
	y, err := strconv.Atoi(s)
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

var providerFanOutPriority = []domain.ProviderName{
	domain.ProviderDeezer,
	domain.ProviderAppleMusic,
	domain.ProviderSpotify,
	domain.ProviderITunes,
	domain.ProviderSoundCloud,
	domain.ProviderLastFM,
}

func orderedProviderNames(providers map[domain.ProviderName]ports.ArtistContentProvider) []domain.ProviderName {
	out := make([]domain.ProviderName, 0, len(providers))
	seen := make(map[domain.ProviderName]bool, len(providers))
	for _, name := range providerFanOutPriority {
		if _, ok := providers[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []domain.ProviderName
	for name := range providers {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].String() < rest[j].String() })
	return append(out, rest...)
}
