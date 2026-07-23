package app

import (
	"context"
	"sort"
	"strconv"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

// detailRerunSearchLimit caps the entity-resolution search — the phone opens one
// artist, so we only need the top artist result and its seed ids.
const detailRerunSearchLimit = 20

// detailReRunner reproduces the mobile client's artist-detail flow so the operator
// console can show exactly what the app renders when it opens an artist — the gap
// the search-only /rerun leaves (detail is a separate pipeline). It resolves the
// top artist entity through the live search Service, then fans out the SAME
// per-seed content calls useArtistContent makes (albums: deezer/soundcloud/itunes
// with the name; top-tracks: deezer with the name, soundcloud + Last.fm-by-MBID
// without) and applies the SAME client-side merge (dedupe by title, keep highest
// track-count, newest-first). Both services are the production instances, so
// identity resolution, the MB anchor, and the caches all match the phone.
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

// rawSeed is one seed provider's un-projected content response, kept as domain
// results so the client-merge can read track-count and release date before the
// display projection.
type rawSeed struct {
	provider   string
	externalID string
	status     string
	items      []domain.SearchResult
}

// ReRunDetail satisfies adminHandler.DetailReRunner.
func (dr *detailReRunner) ReRunDetail(ctx context.Context, query string) (adminHandler.DetailReRunResult, error) {
	start := time.Now()
	entity, ok := dr.resolveTopArtist(ctx, query)
	if !ok {
		return adminHandler.DetailReRunResult{Query: query, TookMs: time.Since(start).Milliseconds()}, nil
	}

	byProvider := seedIDsByProvider(entity.Sources)
	albumSeeds := dr.albumFanOut(ctx, byProvider, entity.Title)
	trackSeeds := dr.trackFanOut(ctx, byProvider, entity.MBID, entity.Title)

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

// resolveTopArtist runs the real search and returns the top artist result — the
// entity the phone would hand to the detail screen, with its seed sources + MBID.
func (dr *detailReRunner) resolveTopArtist(ctx context.Context, query string) (domain.SearchResult, bool) {
	sq, err := domain.NewSearchQuery(query, map[domain.ResultKind]bool{domain.ResultKindArtist: true}, detailRerunSearchLimit)
	if err != nil {
		return domain.SearchResult{}, false
	}
	for _, r := range dr.searchSvc.InspectSearch(ctx, sq) {
		if r.Kind == domain.ResultKindArtist {
			return r, true
		}
	}
	return domain.SearchResult{}, false
}

// albumFanOut mirrors useArtistAlbums: Deezer, SoundCloud, iTunes seeds (in that
// precedence order), each queried with the artist name, capped at 100.
func (dr *detailReRunner) albumFanOut(ctx context.Context, byProvider map[string]string, name string) []rawSeed {
	var seeds []rawSeed
	for _, provider := range []string{"deezer", "soundcloud", "itunes"} {
		if id, ok := byProvider[provider]; ok {
			seeds = append(seeds, dr.fetchAlbums(ctx, provider, id, name))
		}
	}
	return seeds
}

// trackFanOut mirrors useArtistTopTracks: Deezer with the name, SoundCloud without
// (its id is unambiguous), and Last.fm keyed by MBID without a name (identity-safe).
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

// mergeAlbumsLikeClient mirrors dedupAlbumsByTitle + sortByReleaseDateDesc: union
// every ok seed's items in seed order, collapse by normalized title keeping the
// highest track-count variant (union its sources), then order newest-first.
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

// mergeTracksLikeClient mirrors dedupeTracksByTitle + slice(0,5): union in seed
// order (Deezer precedence), keep the first of each normalized title, cap at 5.
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

// mergeAlbumPair keeps the higher-track-count variant (dedupAlbumsByTitle) and
// unions both variants' sources so the merged row shows every contributing seed.
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

// sortReleasesByDateDesc mirrors the service's sortAlbumsByReleaseDateDesc (a
// private helper it can't import): newest-first by ISO release date, else bare
// year, undated sinking to the end; stable so equal-date order is kept.
func sortReleasesByDateDesc(items []domain.SearchResult) {
	sort.SliceStable(items, func(i, j int) bool {
		ki, kj := releaseSortKey(items[i]), releaseSortKey(items[j])
		if ki == "" || kj == "" {
			return ki != "" && kj == ""
		}
		return ki > kj
	})
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

// seedIDsByProvider mirrors the client's sources.find(provider): the first seed id
// per provider name, keyed by provider string so the fan-out matches useArtistContent.
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
