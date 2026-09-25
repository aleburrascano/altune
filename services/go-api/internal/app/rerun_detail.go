package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"

	discoveryService "altune/go-api/internal/discovery/service"
)

const detailRerunSearchLimit = 20

type rawSeed struct {
	provider   domain.ProviderName
	externalID string
	status     domain.ProviderStatus
	err        string
	items      []domain.SearchResult
}

func reRunDetail(
	ctx context.Context,
	searchSvc *discoveryService.Service,
	artistSvc *discoveryService.GetArtistContentService,
	budget time.Duration,
	query string,
) (requeststore.DetailReRunResult, error) {
	start := time.Now()
	entity, ok, err := resolveTopArtist(ctx, searchSvc, query)
	if err != nil {
		return requeststore.DetailReRunResult{}, err
	}
	if !ok {
		return requeststore.DetailReRunResult{Query: query, TookMs: time.Since(start).Milliseconds()}, nil
	}

	byProvider := seedIDsByProvider(entity.Sources)
	albumSeeds, trackSeeds := fanOutSeeds(ctx, artistSvc, budget, byProvider, entity)

	return requeststore.DetailReRunResult{
		Query:      query,
		Resolved:   detailEntity(entity, byProvider),
		AlbumSeeds: projectSeeds(albumSeeds),
		TrackSeeds: projectSeeds(trackSeeds),
		Albums:     projectDetailItems(mergeAlbumSeeds(albumSeeds)),
		TopTracks:  projectDetailItems(mergeTrackSeeds(trackSeeds)),
		TookMs:     time.Since(start).Milliseconds(),
	}, nil
}

// fanOutSeeds runs the sequential provider fan-out under one aggregate wall-time
// budget, so slow providers cannot compound past it (production passes
// inspectorBudget).
func fanOutSeeds(ctx context.Context, artistSvc *discoveryService.GetArtistContentService, budget time.Duration, byProvider map[domain.ProviderName]string, entity domain.SearchResult) (albumSeeds, trackSeeds []rawSeed) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	albumSeeds = albumFanOut(ctx, artistSvc, byProvider, entity.Title)
	trackSeeds = trackFanOut(ctx, artistSvc, byProvider, entity.MBID, entity.Title)
	return albumSeeds, trackSeeds
}

func resolveTopArtist(ctx context.Context, searchSvc *discoveryService.Service, query string) (domain.SearchResult, bool, error) {
	sq, err := domain.NewSearchQuery(query, map[domain.ResultKind]bool{domain.ResultKindArtist: true}, detailRerunSearchLimit)
	if err != nil {
		return domain.SearchResult{}, false, invalidInspectorInput(err)
	}
	results, statuses := searchSvc.InspectSearchWithStatuses(ctx, sq)
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

func albumFanOut(ctx context.Context, artistSvc *discoveryService.GetArtistContentService, byProvider map[domain.ProviderName]string, name string) []rawSeed {
	var seeds []rawSeed
	for _, provider := range []domain.ProviderName{domain.ProviderDeezer, domain.ProviderSoundCloud, domain.ProviderITunes} {
		if id, ok := byProvider[provider]; ok {
			seeds = append(seeds, fetchAlbums(ctx, artistSvc, provider, id, name))
		}
	}
	return seeds
}

func trackFanOut(ctx context.Context, artistSvc *discoveryService.GetArtistContentService, byProvider map[domain.ProviderName]string, mbid, name string) []rawSeed {
	var seeds []rawSeed
	if id, ok := byProvider[domain.ProviderDeezer]; ok {
		seeds = append(seeds, fetchTopTracks(ctx, artistSvc, domain.ProviderDeezer, id, name))
	}
	if id, ok := byProvider[domain.ProviderSoundCloud]; ok {
		seeds = append(seeds, fetchTopTracks(ctx, artistSvc, domain.ProviderSoundCloud, id, ""))
	}
	if mbid != "" {
		seeds = append(seeds, fetchTopTracks(ctx, artistSvc, domain.ProviderLastFM, mbid, ""))
	}
	return seeds
}

func fetchAlbums(ctx context.Context, artistSvc *discoveryService.GetArtistContentService, provider domain.ProviderName, id, name string) rawSeed {
	resp, err := artistSvc.GetAlbums(ctx, provider, id, name, 100)
	return logSeedError(ctx, seedFrom(provider, id, resp, err))
}

const topTracksLimit = 5

func fetchTopTracks(ctx context.Context, artistSvc *discoveryService.GetArtistContentService, provider domain.ProviderName, id, name string) rawSeed {
	resp, err := artistSvc.GetTopTracks(ctx, provider, id, name, topTracksLimit)
	return logSeedError(ctx, seedFrom(provider, id, resp, err))
}

func seedFrom(provider domain.ProviderName, id string, resp *discoveryService.ContentFetchResponse, err error) rawSeed {
	if err != nil || resp == nil {
		msg := "empty provider response"
		if err != nil {
			// Mirrors reRun: a transport *url.Error embeds the request URL,
			// and this string reaches both the log and the admin JSON.
			msg = redact.Secrets(err.Error())
		}
		return rawSeed{provider: provider, externalID: id, status: domain.ProviderStatusError, err: msg}
	}
	return rawSeed{provider: provider, externalID: id, status: resp.Status, items: resp.Items}
}

// logSeedError surfaces a failed seed fetch at the call site instead of
// dropping it. It mirrors fanOutRerun, which captures the same class of error
// into ProviderTrace.Err, so ReRunDetail's operator diagnostic can explain why
// a provider contributed nothing.
func logSeedError(ctx context.Context, seed rawSeed) rawSeed {
	if seed.err != "" {
		slog.WarnContext(ctx, "rerun_detail.seed_fetch_failed",
			"provider", seed.provider.String(), "external_id", seed.externalID, "error", redact.Secrets(seed.err))
	}
	return seed
}
