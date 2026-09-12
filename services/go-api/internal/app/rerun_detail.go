package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	"context"
	"log/slog"
	"time"

	discoveryService "altune/go-api/internal/discovery/service"
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
	err        string
	items      []domain.SearchResult
}

func (dr *detailReRunner) ReRunDetail(ctx context.Context, query string) (requeststore.DetailReRunResult, error) {
	start := time.Now()
	entity, ok, err := dr.resolveTopArtist(ctx, query)
	if err != nil {
		return requeststore.DetailReRunResult{}, err
	}
	if !ok {
		return requeststore.DetailReRunResult{Query: query, TookMs: time.Since(start).Milliseconds()}, nil
	}

	byProvider := seedIDsByProvider(entity.Sources)
	albumSeeds, trackSeeds := dr.fanOutSeeds(ctx, byProvider, entity)

	return requeststore.DetailReRunResult{
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
	results, statuses := dr.searchSvc.InspectSearchWithStatuses(ctx, sq)
	for _, r := range results {
		if r.Kind == domain.ResultKindArtist {
			return r, true, nil
		}
	}
	if discoveryService.AllProvidersFailed(statuses) {
		return domain.SearchResult{}, false, discoveryService.ErrAllProvidersFailed
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
		return logSeedError(ctx, seedFrom(provider, id, nil, err))
	}
	resp, err := dr.artistSvc.GetAlbums(ctx, pn, id, name, 100)
	return logSeedError(ctx, seedFrom(provider, id, resp, err))
}

func (dr *detailReRunner) fetchTopTracks(ctx context.Context, provider, id, name string) rawSeed {
	pn, err := domain.ParseProviderName(provider)
	if err != nil {
		return logSeedError(ctx, seedFrom(provider, id, nil, err))
	}
	resp, err := dr.artistSvc.GetTopTracks(ctx, pn, id, name, 5)
	return logSeedError(ctx, seedFrom(provider, id, resp, err))
}

func seedFrom(provider, id string, resp *discoveryService.ContentFetchResponse, err error) rawSeed {
	if err != nil || resp == nil {
		msg := "empty provider response"
		if err != nil {
			msg = err.Error()
		}
		return rawSeed{provider: provider, externalID: id, status: "error", err: msg}
	}
	return rawSeed{provider: provider, externalID: id, status: resp.Status.String(), items: resp.Items}
}

// logSeedError surfaces a failed seed fetch at the call site instead of
// dropping it. It mirrors fanOutRerun, which captures the same class of error
// into ProviderTrace.Err, so ReRunDetail's operator diagnostic can explain why
// a provider contributed nothing.
func logSeedError(ctx context.Context, seed rawSeed) rawSeed {
	if seed.err != "" {
		slog.WarnContext(ctx, "rerun_detail.seed_fetch_failed",
			"provider", seed.provider, "external_id", seed.externalID, "error", seed.err)
	}
	return seed
}
