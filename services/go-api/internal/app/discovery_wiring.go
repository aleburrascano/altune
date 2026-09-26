package app

import (
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
	searchSvc      *discoveryService.Service
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
	albumSvc, relatedSvc := a.buildAlbumAndRelatedServices(cf, breaker)
	return discoveryContentStaging{
		featuredBridge: buildFeaturedBridge(cf, sharedMB),
		albumSvc:       albumSvc,
		artistSvc:      a.buildArtistContentService(cf, sharedMB, consensusSvc, breaker, eventStore),
		relatedSvc:     relatedSvc,
		suggestSvc:     discoveryService.NewSuggestService(vocabStore),
	}
}

func buildFeaturedBridge(cf clientFactory, sharedMB *providers.MusicBrainzAdapter) *discoverybridge.FeaturedResolver {
	featuredDeezer := providers.NewDeezerAdapter(cf.discovery())
	featuredResolver := discoveryService.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = discoveryService.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	return discoverybridge.NewFeaturedResolver(sharedFeaturedResolver{inner: featuredResolver})
}

func (a *App) buildAlbumAndRelatedServices(
	cf clientFactory,
	breaker *discoveryService.CircuitBreaker,
) (*discoveryService.GetAlbumTracksService, *discoveryService.GetRelatedTracksService) {
	deezerContent := providers.NewDeezerAdapter(cf.discovery())
	albumProviders, relatedProviders := a.buildAlbumContentProviders(cf, deezerContent)

	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders,
		discoveryService.WithRelatedCircuitBreaker(breaker))
	albumSvc := discoveryService.NewGetAlbumTracksService(
		albumProviders,
		discoveryService.WithTrackFeatured(deezerContent),
		discoveryService.WithAlbumFallbackSearcher(deezerContent),
		discoveryService.WithAlbumCircuitBreaker(breaker),
	)
	return albumSvc, relatedSvc
}

func (a *App) buildAlbumContentProviders(
	cf clientFactory,
	deezerContent *providers.DeezerAdapter,
) (map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider, map[string]discoveryPorts.RelatedTracksProvider) {
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
		relatedProviders[string(discoveryDomain.ProviderKeySoundCloud)] = soundcloudContent
	}
	return albumProviders, relatedProviders
}

func (a *App) buildArtistContentService(
	cf clientFactory,
	sharedMB *providers.MusicBrainzAdapter,
	consensusSvc *discoveryService.ConsensusService,
	breaker *discoveryService.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) *discoveryService.GetArtistContentService {
	artistProviders := buildArtistContentProviders(cf, a.cfg)
	artistContentOpts := a.buildArtistContentOptions(sharedMB, consensusSvc, breaker, eventStore)
	return discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
}

func (a *App) buildArtistContentOptions(
	sharedMB *providers.MusicBrainzAdapter,
	consensusSvc *discoveryService.ConsensusService,
	breaker *discoveryService.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) []discoveryService.ArtistContentOption {
	artistContentOpts := []discoveryService.ArtistContentOption{
		discoveryService.WithConsensusService(consensusSvc),
		discoveryService.WithContentCircuitBreaker(breaker),
	}
	if eventStore != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithContentEventStore(eventStore))
	}
	if opt := a.contentIdentityStoreOption(); opt != nil {
		artistContentOpts = append(artistContentOpts, opt)
	}
	if sharedMB != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithMBAnchor(sharedMB))
	}
	return artistContentOpts
}

func (a *App) contentIdentityStoreOption() discoveryService.ArtistContentOption {
	if a.pool == nil {
		return nil
	}
	return discoveryService.WithContentIdentityStore(
		discoveryCacheAdapters.NewRedisIdentityStore(
			discoveryPersistence.NewPgxIdentityStore(a.pool),
			a.redisClient,
			cacheSignalOption(),
		),
	)
}

func (a *App) wireDiscoveryEnrichment(cf clientFactory, sharedMB *providers.MusicBrainzAdapter) *discoveryEnrich.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient, cacheSignalOption())
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
	a.startDiscographyPrune(ctx, eventStore)
	a.startVocabularyRefresh(ctx, cf, vocabStore)
}

func (a *App) searchActivityOptions() []discoveryService.Option {
	if a.eventTap == nil {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithSearchActivityFeed(a.eventTap)}
}

func (a *App) recordEventActivityOptions() []func(*discoveryService.RecordEventService) {
	if a.eventTap == nil {
		return nil
	}
	return []func(*discoveryService.RecordEventService){discoveryService.WithRecordEventActivityFeed(a.eventTap)}
}

func (a *App) buildDiscoveryHandler(cf clientFactory, services discoveryHandler.DiscoveryServices) *discoveryHandler.DiscoveryHandler {
	discoveryH := discoveryHandler.NewDiscoveryHandler(services)
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers(cf))
	return discoveryH
}

func (a *App) wireDiscovery(ctx context.Context, cf clientFactory) discoveryWiring {
	sharedMB := buildMusicBrainzAdapter(cf, a.cfg)
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
		cf.roundTripper(),
		vocabStore,
		a.searchActivityOptions()...,
	)
	// The search service owns detached background work (identity-bridge
	// persistence, telemetry emit, vocab ingest) on context.WithoutCancel, so it
	// outlives request cancellation. Hold the reference so Run()'s shutdown can
	// drain it via WaitForBackground() before cleanup() closes the pool/Redis.
	a.searchSvc = searchSvc
	// The content-fetch services share the search fan-out's breaker, so a
	// provider proven down on either path is short-circuited on both.
	consensusSvc := a.wireDiscoveryConsensus(cf, sharedMB, searchSvc.CircuitBreaker())
	content := a.wireDiscoveryContent(cf, sharedMB, vocabStore, consensusSvc, searchSvc.CircuitBreaker(), eventStore)

	eventSvc := discoveryService.NewRecordEventService(eventStore, a.recordEventActivityOptions()...)
	favoritesSvc := discoveryService.NewFavoritesService(
		discoveryPersistence.NewPgxFavoritesRepository(a.pool),
	)

	enrichSvc := a.wireDiscoveryEnrichment(cf, sharedMB)

	discoveryH := a.buildDiscoveryHandler(cf, discoveryHandler.DiscoveryServices{
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
		searchSvc:      searchSvc,
		featuredBridge: content.featuredBridge,
	}
}

func BuildConsensusProviders(cfg *config.Config, transport http.RoundTripper) []discoveryService.ConsensusProvider {
	cf := newClientFactory(transport)
	var consensusProviders []discoveryService.ConsensusProvider
	add := func(key discoveryDomain.ProviderKey, provider discoveryDomain.ProviderName, fetcher func(context.Context, string) ([]discoveryDomain.SearchResult, error)) {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{Name: string(key), Provider: provider, Fetcher: fetcher})
	}

	if cfg.HasLastFM() {
		lfm := providers.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
		add(discoveryDomain.ProviderKeyLastFM, discoveryDomain.ProviderLastFM, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return lfm.GetArtistAlbums(ctx, discoveryDomain.ProviderLastFM, artistName)
		})
	}
	if mb := buildMusicBrainzAdapter(cf, cfg); mb != nil {
		add(discoveryDomain.ProviderKeyMusicBrainz, discoveryDomain.ProviderMusicBrainz, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return mb.ListArtistDiscography(ctx, artistName)
		})
	}
	if cfg.HasDiscogs() {
		discogs := providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		add(discoveryDomain.ProviderKeyDiscogs, discoveryDomain.ProviderDiscogs, discogsConsensusFetcher(discogs))
	}
	add(discoveryDomain.ProviderKeyITunes, discoveryDomain.ProviderITunes, albumSearchFetcher(providers.NewITunesAdapter(cf.discovery())))
	if cfg.HasYouTubeMusic() {
		ytmusic := providers.NewYouTubeMusicAdapter(cf.roundTripper())
		add(discoveryDomain.ProviderKeyYTMusic, discoveryDomain.ProviderYouTube, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return ytmusic.GetArtistAlbums(ctx, discoveryDomain.ProviderYouTube, artistName)
		})
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		add(discoveryDomain.ProviderKeySoundCloud, discoveryDomain.ProviderSoundCloud, albumSearchFetcher(sc))
	}
	return consensusProviders
}

func albumSearchFetcher(p interface {
	Search(ctx context.Context, query string, kinds map[discoveryDomain.ResultKind]bool) ([]discoveryDomain.SearchResult, error)
},
) func(context.Context, string) ([]discoveryDomain.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
		return p.Search(ctx, artistName, map[discoveryDomain.ResultKind]bool{discoveryDomain.ResultKindAlbum: true})
	}
}

func discogsConsensusFetcher(discogs *providers.DiscogsAdapter) func(context.Context, string) ([]discoveryDomain.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
		info, err := discogs.ResolveDiscogsArtist(ctx, artistName, nil)
		if err != nil || info == nil {
			return nil, err
		}
		releases, err := discogs.FetchArtistReleases(ctx, info.ID)
		if err != nil {
			return nil, err
		}
		return discogsReleasesToSearchResults(releases), nil
	}
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
		discoveryCacheAdapters.WithVocabSignal(cacheSignal()),
	)
}
