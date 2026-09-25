package app

import (
	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"net/http"
	"time"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"

	goredis "github.com/redis/go-redis/v9"
)

type discoveryWiring struct {
	handler        *discoveryHandler.DiscoveryHandler
	requestStore   *requeststore.Store
	searchSvc      *discoveryService.Service
	artistSvc      *discoveryService.GetArtistContentService
	featuredBridge *discoverybridge.FeaturedResolver
}

type discoveryContentStaging struct {
	featuredBridge *discoverybridge.FeaturedResolver
	albumSvc       *discoveryService.GetAlbumTracksService
	artistSvc      *discoveryService.GetArtistContentService
	relatedSvc     *discoveryService.GetRelatedTracksService
	suggestSvc     *discoveryService.SuggestService
}

// sharedFeaturedResolver maps discovery's own FeaturedArtist onto the shared
// value the catalog bridge speaks, so catalog never imports discovery/domain.
type sharedFeaturedResolver struct {
	inner *discoveryService.FeaturedArtistResolver
}

func (r sharedFeaturedResolver) Resolve(ctx context.Context, artist, title string) ([]shared.FeaturedArtist, error) {
	feats, err := r.inner.Resolve(ctx, artist, title)
	if err != nil {
		return nil, err
	}
	out := make([]shared.FeaturedArtist, 0, len(feats))
	for _, f := range feats {
		out = append(out, shared.FeaturedArtist{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID, Role: f.Role})
	}
	return out, nil
}

func (a *App) wireDiscoveryConsensus(cf clientFactory, sharedMB *providers.MusicBrainzAdapter, breaker *discoveryService.CircuitBreaker) *discoveryService.ConsensusService {
	consensusProviders := BuildConsensusProviders(a.cfg, cf.roundTripper())

	consensusOpts := []discoveryService.ConsensusOption{discoveryService.WithConsensusCircuitBreaker(breaker)}
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

func (a *App) wireDiscoveryContent(
	cf clientFactory,
	sharedMB *providers.MusicBrainzAdapter,
	vocabStore discoveryPorts.VocabularyStore,
	consensusSvc *discoveryService.ConsensusService,
	breaker *discoveryService.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) discoveryContentStaging {
	featuredDeezer := providers.NewDeezerAdapter(cf.discovery())
	featuredResolver := discoveryService.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = discoveryService.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	featuredBridge := discoverybridge.NewFeaturedResolver(sharedFeaturedResolver{inner: featuredResolver})

	deezerContentClient := cf.discovery()
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)
	itunesContent := providers.NewITunesAdapter(cf.discovery())

	albumProviders := map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider{
		discoveryDomain.ProviderDeezer: deezerContent,
		discoveryDomain.ProviderITunes: itunesContent,
	}
	relatedProviders := map[string]discoveryPorts.RelatedTracksProvider{}
	if am := buildAppleMusicAdapter(cf, a.cfg); am != nil {
		albumProviders[discoveryDomain.ProviderAppleMusic] = am
	}
	if sp := buildSpotifyAdapter(cf, a.cfg); sp != nil {
		albumProviders[discoveryDomain.ProviderSpotify] = sp
	}
	if soundcloudContent := buildSoundCloudAdapter(cf, a.cfg); soundcloudContent != nil {
		albumProviders[discoveryDomain.ProviderSoundCloud] = soundcloudContent
		relatedProviders["soundcloud"] = soundcloudContent
	}
	artistProviders := buildArtistContentProviders(cf, a.cfg)
	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders,
		discoveryService.WithRelatedCircuitBreaker(breaker))

	albumSvc := discoveryService.NewGetAlbumTracksService(
		albumProviders,
		discoveryService.WithTrackFeatured(deezerContent),
		discoveryService.WithAlbumFallbackSearcher(deezerContent),
		discoveryService.WithAlbumCircuitBreaker(breaker),
	)

	var artistContentOpts []discoveryService.ArtistContentOption
	artistContentOpts = append(artistContentOpts,
		discoveryService.WithConsensusService(consensusSvc),
		discoveryService.WithContentCircuitBreaker(breaker),
	)
	if eventStore != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithContentEventStore(eventStore))
	}
	if a.pool != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithContentIdentityStore(
			discoveryCacheAdapters.NewRedisIdentityStore(
				discoveryPersistence.NewPgxIdentityStore(a.pool),
				a.redisClient,
			),
		))
	}
	if sharedMB != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithMBAnchor(sharedMB))
	}
	artistSvc := discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
	suggestSvc := discoveryService.NewSuggestService(vocabStore)

	return discoveryContentStaging{
		featuredBridge: featuredBridge,
		albumSvc:       albumSvc,
		artistSvc:      artistSvc,
		relatedSvc:     relatedSvc,
		suggestSvc:     suggestSvc,
	}
}

func (a *App) wireDiscoveryEnrichment(cf clientFactory, sharedMB *providers.MusicBrainzAdapter) *discoveryEnrich.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient)
	return discoveryEnrich.NewEnrichmentService(
		sharedMB,
		buildArtworkChain(cf, a.cfg),
		enrichmentCache,
		discoveryEnrich.WithMBIDMemo(enrichmentCache),
	)
}

// startDiscoveryBackgroundJobs schedules the detached background work that the
// discovery object graph depends on: behavioral-ranking refresh (leader only),
// corpus refresh, metrics rollup, discography-event retention prune and
// vocabulary refresh. It is kept separate
// from wireDiscovery's object-graph construction so the wiring stays free of
// side effects.
func (a *App) startDiscoveryBackgroundJobs(
	ctx context.Context,
	cf clientFactory,
	searchSvc *discoveryService.Service,
	eventStore *discoveryPersistence.PgxEventStore,
	vocabStore discoveryPorts.VocabularyStore,
) {
	if a.cfg.BehavioralRankingEnabled {
		a.startEveryInstanceTicker(ctx, jobBehavioralRankingRefresh, 30*time.Minute, searchSvc.RefreshBehavioralScores)
	}
	a.startCorpusRefresh(ctx, eventStore)
	a.startMetricsRollup(ctx, discoveryPersistence.NewPgxMetricsRollup(a.pool))
	a.startDiscographyPrune(ctx, eventStore)
	a.startVocabularyRefresh(ctx, cf, vocabStore)
}

func (a *App) searchAdminActivityOptions() []discoveryService.Option {
	if a.eventTap == nil {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithSearchAdminActivity(a.eventTap)}
}

func (a *App) recordEventAdminActivityOptions() []func(*discoveryService.RecordEventService) {
	if a.eventTap == nil {
		return nil
	}
	return []func(*discoveryService.RecordEventService){discoveryService.WithRecordEventAdminActivity(a.eventTap)}
}

func (a *App) buildDiscoveryHandler(tracedClients clientFactory, requestStore *requeststore.Store, services discoveryHandler.DiscoveryServices) *discoveryHandler.DiscoveryHandler {
	discoveryH := discoveryHandler.NewDiscoveryHandler(services)
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers(tracedClients))
	a.providerHealth = providerhealth.NewStore()
	discoveryH.WithProviderHealth(a.providerHealth)
	discoveryH.WithRequestTrace(requestStore)
	return discoveryH
}

func (a *App) wireDiscovery(ctx context.Context, cf clientFactory) discoveryWiring {
	requestStore := requeststore.New()
	correlatedTransport := requeststore.NewCorrelatedTransport(cf.roundTripper(), requestStore)
	// Every request-path adapter is built from this one factory, so each call it
	// makes lands in the caller's trace. The provider counter sits at the base of
	// cf's own transport, so a call is counted exactly once whether or not it is
	// traced; the background jobs keep cf itself, counted and untraced, since
	// they run under no request.
	tracedClients := newClientFactory(correlatedTransport)

	sharedMB := buildMusicBrainzAdapter(tracedClients, a.cfg)
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)

	vocabStore := BuildVocabularyStore(a.redisClient)

	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := discoveryService.NewClearSearchHistoryService(historyRepo)

	searchSvc := BuildSearchServiceWithTransport(
		a.cfg,
		a.pool,
		a.redisClient,
		eventStore,
		correlatedTransport,
		vocabStore,
		a.searchAdminActivityOptions()...,
	)
	// The search service owns detached background work (identity-bridge
	// persistence, telemetry emit, vocab ingest) on context.WithoutCancel, so it
	// outlives request cancellation. Hold the reference so Run()'s shutdown can
	// drain it via WaitForBackground() before cleanup() closes the pool/Redis.
	a.searchSvc = searchSvc
	// The content-fetch services share the search fan-out's breaker, so a
	// provider proven down on either path is short-circuited on both.
	consensusSvc := a.wireDiscoveryConsensus(tracedClients, sharedMB, searchSvc.CircuitBreaker())
	content := a.wireDiscoveryContent(tracedClients, sharedMB, vocabStore, consensusSvc, searchSvc.CircuitBreaker(), eventStore)

	eventSvc := discoveryService.NewRecordEventService(eventStore, a.recordEventAdminActivityOptions()...)
	favoritesSvc := discoveryService.NewFavoritesService(
		discoveryPersistence.NewPgxFavoritesRepository(a.pool),
	)

	enrichSvc := a.wireDiscoveryEnrichment(tracedClients, sharedMB)

	discoveryH := a.buildDiscoveryHandler(tracedClients, requestStore, discoveryHandler.DiscoveryServices{
		Search:       searchSvc,
		History:      historySvc,
		ClearHistory: clearHistorySvc,
		Album:        content.albumSvc,
		Artist:       content.artistSvc,
		Related:      content.relatedSvc,
		Enrich:       enrichSvc,
		Suggest:      content.suggestSvc,
		Event:        eventSvc,
		Favorites:    favoritesSvc,
	})

	a.startDiscoveryBackgroundJobs(ctx, cf, searchSvc, eventStore, vocabStore)

	return discoveryWiring{
		handler:        discoveryH,
		requestStore:   requestStore,
		searchSvc:      searchSvc,
		artistSvc:      content.artistSvc,
		featuredBridge: content.featuredBridge,
	}
}

func BuildConsensusProviders(cfg *config.Config, transport http.RoundTripper) []discoveryService.ConsensusProvider {
	cf := newClientFactory(transport)
	var consensusProviders []discoveryService.ConsensusProvider

	if cfg.HasLastFM() {
		lfm := providers.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name:     "lastfm",
			Provider: discoveryDomain.ProviderLastFM,
			Fetcher: func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
				return lfm.GetArtistAlbums(ctx, discoveryDomain.ProviderLastFM, artistName)
			},
		})
	}
	if mb := buildMusicBrainzAdapter(cf, cfg); mb != nil {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name:     "musicbrainz",
			Provider: discoveryDomain.ProviderMusicBrainz,
			Fetcher: func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
				return mb.ListArtistDiscography(ctx, artistName)
			},
		})
	}
	if cfg.HasDiscogs() {
		discogs := providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name:     "discogs",
			Provider: discoveryDomain.ProviderDiscogs,
			Fetcher: func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
				info, err := discogs.ResolveDiscogsArtist(ctx, artistName, nil)
				if err != nil || info == nil {
					return nil, err
				}
				releases, err := discogs.FetchArtistReleases(ctx, info.ID)
				if err != nil {
					return nil, err
				}
				return discogsReleasesToSearchResults(releases), nil
			},
		})
	}

	itunes := providers.NewITunesAdapter(cf.discovery())
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name:     "itunes",
		Provider: discoveryDomain.ProviderITunes,
		Fetcher: func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return itunes.Search(ctx, artistName, map[discoveryDomain.ResultKind]bool{discoveryDomain.ResultKindAlbum: true})
		},
	})

	if cfg.HasYouTubeMusic() {
		ytmusic := providers.NewYouTubeMusicAdapter(cf.roundTripper())
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name:     "ytmusic",
			Provider: discoveryDomain.ProviderYouTube,
			Fetcher: func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
				return ytmusic.GetArtistAlbums(ctx, discoveryDomain.ProviderYouTube, artistName)
			},
		})
	}

	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name:     "soundcloud",
			Provider: discoveryDomain.ProviderSoundCloud,
			Fetcher: func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
				return sc.Search(ctx, artistName, map[discoveryDomain.ResultKind]bool{discoveryDomain.ResultKindAlbum: true})
			},
		})
	}

	return consensusProviders
}

// discogsReleasesToSearchResults maps Discogs artist releases onto album
// SearchResults, carrying the year and record type through as extras.
func discogsReleasesToSearchResults(releases []discoveryPorts.DiscogsRelease) []discoveryDomain.SearchResult {
	results := make([]discoveryDomain.SearchResult, 0, len(releases))
	for _, r := range releases {
		results = append(results, discoveryDomain.SearchResult{
			Kind:       discoveryDomain.ResultKindAlbum,
			Title:      r.Title,
			RecordType: discoveryDomain.RecordType(r.Type),
			Extras: map[string]any{
				"year": r.Year,
			},
		})
	}
	return results
}

// BuildArtworkChain exposes the production artwork resolver chain to out-of-band
// tools (e.g. cmd/backfillartwork) so they re-resolve covers through the exact
// same corrected chain the live search path uses. It is a thin wrapper over the
// internal wiring; the resolution logic itself lives in the discovery adapters.
func BuildArtworkChain(cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	return buildArtworkChain(newClientFactory(nil), cfg)
}

func buildArtworkChain(cf clientFactory, cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	var artworkResolvers []discoveryPorts.ArtworkResolver
	artworkResolvers = append(artworkResolvers, providers.NewCoverArtArchiveResolver(cf.discovery()))
	if cfg.HasSpotify() {
		artworkResolvers = append(artworkResolvers, providers.NewSpotifyArtworkResolver(cf.discovery()))
	}
	if cfg.HasDiscogs() {
		artworkResolvers = append(artworkResolvers,
			providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent))
	}
	if cfg.HasFanartTV() {
		artworkResolvers = append(artworkResolvers,
			providers.NewFanartTvArtworkResolver(cf.discovery(), cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		artworkResolvers = append(artworkResolvers,
			providers.NewGeniusArtworkResolver(cf.discovery(), cfg.GeniusAccessToken))
	}
	artworkResolvers = append(artworkResolvers,
		providers.NewTheAudioDBAdapter(cf.discovery()),
		providers.NewDeezerAdapter(cf.discovery()),
		providers.NewITunesAdapter(cf.discovery()),
	)
	if cfg.HasYouTubeMusic() {
		artworkResolvers = append(artworkResolvers, providers.NewYouTubeMusicArtworkResolver(cf.roundTripper()))
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		artworkResolvers = append(artworkResolvers, sc)
	}
	return providers.NewChainedArtworkResolver(artworkResolvers...)
}

func BuildVocabularyStore(redisClient *goredis.Client) discoveryPorts.VocabularyStore {
	if redisClient == nil {
		return nil
	}
	return discoveryCacheAdapters.NewVocabularyStore(
		redisClient,
		textnorm.NormalizeForMatch,
		discoveryCacheAdapters.WithMetaphone(phonetics.MetaphoneKey),
	)
}
