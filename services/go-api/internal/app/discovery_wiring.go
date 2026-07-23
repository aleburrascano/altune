package app

import (
	"context"
	"time"

	"log/slog"

	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

// discoveryWiring carries the discovery-context values other stages consume:
// the mounted handler, the operator-surface inputs (request store + live search
// service), and the catalog-facing featured-artist bridge.
type discoveryWiring struct {
	handler        *discoveryHandler.DiscoveryHandler
	requestStore   *requeststore.Store
	searchSvc      *discoveryService.Service
	featuredBridge *discoverybridge.FeaturedResolver
}

// discoveryContentWiring carries the featured-artist bridge and the detail/
// content services (album, artist, related, suggest) wired by
// wireDiscoveryContent.
type discoveryContentWiring struct {
	featuredBridge *discoverybridge.FeaturedResolver
	albumSvc       *discoveryService.GetAlbumTracksService
	artistSvc      *discoveryService.GetArtistContentService
	relatedSvc     *discoveryService.GetRelatedTracksService
	suggestSvc     *discoveryService.SuggestService
}

// wireDiscoveryConsensus builds the multi-provider album consensus service: ALL
// providers are equal sources, merged into a union via the shared
// BuildConsensusProviders so coverage signal B measures the same provider set.
func (a *App) wireDiscoveryConsensus(sharedMB *providers.MusicBrainzAdapter) *discoveryService.ConsensusService {
	consensusProviders := BuildConsensusProviders(a.cfg, nil)

	var consensusOpts []discoveryService.ConsensusOption
	if sharedMB != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithMBAuthority(sharedMB))
	}
	if a.redisClient != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithConsensusCache(
			discoveryCacheAdapters.NewRedisNameKeyedCache[[]discoveryService.ConsensusAlbum](
				a.redisClient,
				"discovery:consensus:v1:",
				"discovery:consensus:neg:v1:",
				discoveryService.DefaultConsensusCacheTTL,
				discoveryService.DefaultConsensusCacheTTL,
				func() []discoveryService.ConsensusAlbum { return nil },
			),
		))
	}
	return discoveryService.NewConsensusService(consensusProviders, consensusOpts...)
}

// wireDiscoveryContent builds the featured-artist bridge and the detail/content
// services: album tracks, artist content (consensus-backed), related tracks,
// and suggest.
func (a *App) wireDiscoveryContent(
	sharedMB *providers.MusicBrainzAdapter,
	vocabStore discoveryPorts.VocabularyStore,
	consensusSvc *discoveryService.ConsensusService,
) discoveryContentWiring {
	// Featured-artist resolver (discovery-sourced) + catalog bridge. The resolver
	// tolerates a nil MB searcher (MusicBrainz not configured) and degrades to
	// Deezer-only; a nil interface (not a typed-nil pointer) keeps that safe.
	featuredDeezer := providers.NewDeezerAdapter(newDiscoveryClient())
	featuredResolver := discoveryService.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = discoveryService.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	featuredBridge := discoverybridge.NewFeaturedResolver(featuredResolver)

	deezerContentClient := newDiscoveryClient()
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)
	// iTunes is a second mainstream source of truth for discography/tracklist
	// (docs/providers/itunes.md cap 5): an iTunes-sourced album/artist result
	// carries its collectionId/artistId, which keys the /lookup content endpoint.
	itunesContent := providers.NewITunesAdapter(newDiscoveryClient())
	// Apple Music + Spotify join the artist-content fan-out through the same
	// ArtistContentProvider interface (see docs/brainstorms/2026-07-22-provider-
	// uniform-interface.md — "stop excluding them"). Apple Music replaces iTunes for
	// artist content: same Apple catalog + ids, but the official Catalog API carries
	// release dates, cover art, and ISRC that the plain iTunes lookup misses. Spotify
	// uses its classic /v1 endpoints with the anonymous web-player token (no
	// persisted-query hash). Both are keyed by their bridged id (Apple via the shared
	// iTunes id; see providerContentID).
	appleMusicContent := providers.NewAppleMusicAdapter(newDiscoveryClient())
	spotifyContent := providers.NewSpotifyAdapter(newDiscoveryClient())

	albumProviders := map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider{
		discoveryDomain.ProviderDeezer: deezerContent,
		discoveryDomain.ProviderITunes: itunesContent,
	}
	artistProviders := map[discoveryDomain.ProviderName]discoveryPorts.ArtistContentProvider{
		discoveryDomain.ProviderDeezer:     deezerContent,
		discoveryDomain.ProviderAppleMusic: appleMusicContent,
		discoveryDomain.ProviderSpotify:    spotifyContent,
		// SoundCloud serves the underground long tail: an artist sourced from
		// SoundCloud carries its numeric user id, which keys these endpoints.
		discoveryDomain.ProviderSoundCloud: providers.NewSoundCloudAPIAdapter(newDiscoveryClient(), nil),
	}
	// Last.fm top-tracks, keyed by MBID (identity-safe) — the client calls it only
	// when the artist has a resolved MBID, so it never falls back to ambiguous
	// name matching. Adds the scrobble-popular layer alongside Deezer/SoundCloud.
	if a.cfg.HasLastFM() {
		artistProviders[discoveryDomain.ProviderLastFM] = providers.NewLastFmAdapter(newDiscoveryClient(), a.cfg.LastFMAPIKey)
	}

	// Related tracks are track-keyed: a SoundCloud-sourced track carries its
	// numeric track id, which keys /tracks/{id}/related. SoundCloud-only today.
	relatedProviders := map[string]discoveryPorts.RelatedTracksProvider{
		"soundcloud": providers.NewSoundCloudAPIAdapter(newDiscoveryClient(), nil),
	}
	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders)

	albumSvc := discoveryService.NewGetAlbumTracksService(
		albumProviders,
		discoveryService.WithTrackFeatured(deezerContent),
		discoveryService.WithAlbumFallbackSearcher(deezerContent),
	)

	var artistContentOpts []discoveryService.ArtistContentOption
	artistContentOpts = append(artistContentOpts, discoveryService.WithConsensusService(consensusSvc))
	// Identity-first detail (DETAIL_IDENTITY_FIRST, default off): give the artist-
	// content service the same durable identity store the search path writes, so it
	// can reverse-resolve a single provider id into the artist's full cross-provider
	// identity and fan out by each provider's own id. Store wired whenever a pool
	// exists; the fan-out only activates behind the flag.
	if a.pool != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithContentIdentityStore(
			discoveryCacheAdapters.NewRedisIdentityStore(
				discoveryPersistence.NewPgxIdentityStore(a.pool),
				a.redisClient,
			),
		))
	}
	if a.cfg.DetailIdentityFirst {
		artistContentOpts = append(artistContentOpts, discoveryService.WithIdentityFirst())
	}
	artistSvc := discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
	suggestSvc := discoveryService.NewSuggestService(vocabStore)

	return discoveryContentWiring{
		featuredBridge: featuredBridge,
		albumSvc:       albumSvc,
		artistSvc:      artistSvc,
		relatedSvc:     relatedSvc,
		suggestSvc:     suggestSvc,
	}
}

// wireDiscoveryEnrichment builds the detail-open MusicBrainz enrichment service
// (genres/year/rating/external-ids + the HD MBID-keyed cover via the existing
// artwork chain). Returns nil when MusicBrainz is not configured — the handler
// degrades to an empty DTO.
func (a *App) wireDiscoveryEnrichment(sharedMB *providers.MusicBrainzAdapter) *discoveryService.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient)
	return discoveryService.NewEnrichmentService(
		sharedMB,
		buildArtworkChain(clientFactory{}, a.cfg),
		enrichmentCache,
		// Memoize each name resolution so the search path can attach the MBID to
		// a non-MB result later (cap 5 warm).
		discoveryService.WithMBIDMemo(enrichmentCache),
	)
}

// wireDiscovery builds the discovery context: search pipeline, content/detail
// services, telemetry-fed background tickers, and the mounted handler.
func (a *App) wireDiscovery(ctx context.Context) discoveryWiring {
	var sharedMB *providers.MusicBrainzAdapter
	if a.cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			newDiscoveryClient(),
			a.cfg.MusicBrainzUserAgent,
		)
	}
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)

	// vocabStore is shared by suggest + the periodic vocabulary refresh; the
	// search pipeline builds its own inside BuildSearchService.
	vocabStore := BuildVocabularyStore(a.redisClient)

	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := discoveryService.NewClearSearchHistoryService(historyRepo)

	// Multi-provider consensus feeds artist-content, so it's wired before content.
	consensusSvc := a.wireDiscoveryConsensus(sharedMB)
	content := a.wireDiscoveryContent(sharedMB, vocabStore, consensusSvc)

	// The recording transport wraps the shared live transport so every provider
	// call on a correlated request is captured into the drill-down store, keyed by
	// correlation id. Bounded + degrades silently — never affects the search path.
	requestStore := requeststore.New()
	searchSvc := BuildSearchServiceWithTransport(
		a.cfg,
		a.pool,
		a.redisClient,
		eventStore,
		requeststore.NewTransport(defaultLiveTransport, requestStore),
		vocabStore,
		false,
	)

	// Behavioral-ranking refresh: when the flag is on, recompute the satisfaction
	// score map off the request path on a ticker. Inert (the option is unset)
	// otherwise. Bound to the app context so it exits on graceful shutdown.
	if a.cfg.BehavioralRankingEnabled {
		searchSvc.StartBehavioralRefresh(ctx, 30*time.Minute)
		slog.Info("behavioral ranking refresh started")
	}

	// Self-growing eval corpus: when a path is configured, nightly-materialize the
	// behavioral labels (search→engagement positives, wrong_album hard negatives)
	// into the eval corpus format. Off when the path is empty.
	a.startCorpusRefresh(ctx, eventStore)

	// Mission Control metrics rollup: persist the daily aggregate gauges so the
	// console's history survives restart. Always on (cheap aggregate upsert).
	a.startMetricsRollup(ctx, discoveryPersistence.NewPgxMetricsRollup(a.pool))

	eventSvc := discoveryService.NewRecordEventService(eventStore)

	enrichSvc := a.wireDiscoveryEnrichment(sharedMB)

	discoveryH := discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{
		Search:       searchSvc,
		History:      historySvc,
		ClearHistory: clearHistorySvc,
		Album:        content.albumSvc,
		Artist:       content.artistSvc,
		Related:      content.relatedSvc,
		Enrich:       enrichSvc,
		Suggest:      content.suggestSvc,
		Event:        eventSvc,
	})
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers())
	a.providerHealth = providerhealth.NewStore()
	discoveryH.WithProviderHealth(a.providerHealth)
	discoveryH.WithRequestTrace(requestStore)

	a.startVocabularyRefresh(vocabStore)

	return discoveryWiring{
		handler:        discoveryH,
		requestStore:   requestStore,
		searchSvc:      searchSvc,
		featuredBridge: content.featuredBridge,
	}
}
