package app

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

const detailRerunSearchLimit = 20

// detailReRunBudget caps the total wall time of the sequential provider
// fan-out. Without it, six back-to-back no-timeout provider calls (each bounded
// only by its own 10-15s HTTP client, up to 3 retries) can compound into a
// multi-minute stuck admin request. It is a var so tests can shrink it.
var detailReRunBudget = 30 * time.Second

type detailReRunner struct {
	searchSvc *discoveryService.Service
	artistSvc *discoveryService.GetArtistContentService
}

func (a *App) buildDetailReRunner(
	searchSvc *discoveryService.Service,
	artistSvc *discoveryService.GetArtistContentService,
) *detailReRunner {
	return &detailReRunner{searchSvc: searchSvc, artistSvc: artistSvc}
}

type rawSeed struct {
	provider   string
	externalID string
	status     string
	items      []domain.SearchResult
}

func (dr *detailReRunner) ReRunDetail(ctx context.Context, query string) (adminHandler.DetailReRunResult, error) {
	start := time.Now()
	entity, ok, err := dr.resolveTopArtist(ctx, query)
	if err != nil {
		return adminHandler.DetailReRunResult{}, err
	}
	if !ok {
		return adminHandler.DetailReRunResult{Query: query, TookMs: time.Since(start).Milliseconds()}, nil
	}

	byProvider := seedIDsByProvider(entity.Sources)
	albumSeeds, trackSeeds := dr.fanOutSeeds(ctx, byProvider, entity)

	return adminHandler.DetailReRunResult{
		Query:      query,
		Resolved:   detailEntity(entity, byProvider),
		AlbumSeeds: projectSeeds(albumSeeds),
		TrackSeeds: projectSeeds(trackSeeds),
		Albums:     projectDetailItems(mergeAlbumsLikeClient(albumSeeds)),
		TopTracks:  projectDetailItems(mergeTracksLikeClient(trackSeeds)),
		TookMs:     time.Since(start).Milliseconds(),
	}, nil
}

func (dr *detailReRunner) fanOutSeeds(ctx context.Context, byProvider map[string]string, entity domain.SearchResult) (albumSeeds, trackSeeds []rawSeed) {
	ctx, cancel := context.WithTimeout(ctx, detailReRunBudget)
	defer cancel()
	albumSeeds = dr.albumFanOut(ctx, byProvider, entity.Title)
	trackSeeds = dr.trackFanOut(ctx, byProvider, entity.MBID, entity.Title)
	return albumSeeds, trackSeeds
}

func (dr *detailReRunner) resolveTopArtist(ctx context.Context, query string) (domain.SearchResult, bool, error) {
	sq, err := domain.NewSearchQuery(query, map[domain.ResultKind]bool{domain.ResultKindArtist: true}, detailRerunSearchLimit)
	if err != nil {
		return domain.SearchResult{}, false, err
	}
	for _, r := range dr.searchSvc.InspectSearch(ctx, sq) {
		if r.Kind == domain.ResultKindArtist {
			return r, true, nil
		}
	}
	return domain.SearchResult{}, false, nil
}

func (dr *detailReRunner) albumFanOut(ctx context.Context, byProvider map[string]string, name string) []rawSeed {
	var seeds []rawSeed
	for _, provider := range []string{"deezer", "soundcloud", "itunes"} {
		if id, ok := byProvider[provider]; ok {
			seeds = append(seeds, dr.fetchAlbums(ctx, provider, id, name))
		}
	}
	return seeds
}

func (dr *detailReRunner) trackFanOut(ctx context.Context, byProvider map[string]string, mbid, name string) []rawSeed {
	var seeds []rawSeed
	if id, ok := byProvider["deezer"]; ok {
		seeds = append(seeds, dr.fetchTopTracks(ctx, "deezer", id, name))
	}
	if id, ok := byProvider["soundcloud"]; ok {
		seeds = append(seeds, dr.fetchTopTracks(ctx, "soundcloud", id, ""))
	}
	if mbid != "" {
		seeds = append(seeds, dr.fetchTopTracks(ctx, "lastfm", mbid, ""))
	}
	return seeds
}

func (dr *detailReRunner) fetchAlbums(ctx context.Context, provider, id, name string) rawSeed {
	pn, err := domain.ParseProviderName(provider)
	if err != nil {
		return rawSeed{provider: provider, externalID: id, status: "error"}
	}
	resp, err := dr.artistSvc.GetAlbums(ctx, pn, id, name, 100)
	return seedFrom(provider, id, resp, err)
}

func (dr *detailReRunner) fetchTopTracks(ctx context.Context, provider, id, name string) rawSeed {
	pn, err := domain.ParseProviderName(provider)
	if err != nil {
		return rawSeed{provider: provider, externalID: id, status: "error"}
	}
	resp, err := dr.artistSvc.GetTopTracks(ctx, pn, id, name, 5)
	return seedFrom(provider, id, resp, err)
}

func seedFrom(provider, id string, resp *discoveryService.ContentFetchResponse, err error) rawSeed {
	if err != nil || resp == nil {
		return rawSeed{provider: provider, externalID: id, status: "error"}
	}
	return rawSeed{provider: provider, externalID: id, status: resp.Status.String(), items: resp.Items}
}

func mergeAlbumsLikeClient(seeds []rawSeed) []domain.SearchResult {
	seen := map[string]int{}
	var out []domain.SearchResult
	for _, s := range okSeedItems(seeds) {
		key := textnorm.NormalizeForMatch(s.Title)
		if i, ok := seen[key]; ok {
			out[i] = mergeAlbumPair(out[i], s)
			continue
		}
		seen[key] = len(out)
		out = append(out, s)
	}
	sortReleasesByDateDesc(out)
	return out
}

func mergeTracksLikeClient(seeds []rawSeed) []domain.SearchResult {
	seen := map[string]bool{}
	var out []domain.SearchResult
	for _, t := range okSeedItems(seeds) {
		key := textnorm.NormalizeForMatch(t.Title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func okSeedItems(seeds []rawSeed) []domain.SearchResult {
	var flat []domain.SearchResult
	for _, s := range seeds {
		if s.status == "ok" {
			flat = append(flat, s.items...)
		}
	}
	return flat
}

func mergeAlbumPair(existing, incoming domain.SearchResult) domain.SearchResult {
	winner := existing
	if incoming.TrackCount > existing.TrackCount {
		winner = incoming
	}
	winner.Sources = unionSourceRefs(existing.Sources, incoming.Sources)
	return winner
}

func unionSourceRefs(a, b []domain.SourceRef) []domain.SourceRef {
	seen := map[string]bool{}
	out := make([]domain.SourceRef, 0, len(a)+len(b))
	for _, s := range append(append([]domain.SourceRef{}, a...), b...) {
		key := s.Provider.String() + "|" + s.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func sortReleasesByDateDesc(items []domain.SearchResult) {
	sort.SliceStable(items, func(i, j int) bool {
		return releaseKeyNewer(releaseSortKey(items[i]), releaseSortKey(items[j]))
	})
}

// releaseKeyNewer reports whether key ki sorts newer-first than kj. Keys are
// either a full ISO date or a bare year; it normalizes them to the same
// precision first, so a year-only value ties with a same-year full date
// instead of always sorting older than it.
func releaseKeyNewer(ki, kj string) bool {
	if ki == "" || kj == "" {
		return ki != "" && kj == ""
	}
	if len(ki) != len(kj) && releaseYear(ki) == releaseYear(kj) {
		return false
	}
	return ki > kj
}

func releaseYear(key string) string {
	if i := strings.IndexByte(key, '-'); i > 0 {
		return key[:i]
	}
	return key
}

func releaseSortKey(r domain.SearchResult) string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year)
	}
	return ""
}

func detailEntity(entity domain.SearchResult, byProvider map[string]string) *adminHandler.DetailEntity {
	return &adminHandler.DetailEntity{
		Title:    entity.Title,
		Subtitle: entity.Subtitle,
		MBID:     entity.MBID,
		Sources:  byProvider,
	}
}

func seedIDsByProvider(sources []domain.SourceRef) map[string]string {
	m := make(map[string]string, len(sources))
	for _, s := range sources {
		name := s.Provider.String()
		if _, exists := m[name]; !exists {
			m[name] = s.ExternalID
		}
	}
	return m
}

func projectSeeds(seeds []rawSeed) []adminHandler.DetailSeedGroup {
	out := make([]adminHandler.DetailSeedGroup, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, adminHandler.DetailSeedGroup{
			Provider:   s.provider,
			ExternalID: s.externalID,
			Status:     s.status,
			Items:      projectDetailItems(s.items),
		})
	}
	return out
}

func projectDetailItems(items []domain.SearchResult) []adminHandler.DetailItemRow {
	out := make([]adminHandler.DetailItemRow, 0, len(items))
	for _, it := range items {
		out = append(out, adminHandler.DetailItemRow{
			Title:      it.Title,
			Subtitle:   it.Subtitle,
			Year:       it.Year,
			TrackCount: it.TrackCount,
			RecordType: detailExtraString(it, "record_type"),
			ImageURL:   it.ImageURL,
			Sources:    seedProviderNames(it.Sources),
		})
	}
	return out
}

func seedProviderNames(sources []domain.SourceRef) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Provider.String())
	}
	return out
}

func detailExtraString(r domain.SearchResult, key string) string {
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}
