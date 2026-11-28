package search

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/applemusic"
	"altune/go-api/internal/search/internal/adapters/artworkfallback"
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/coverartarchive"
	"altune/go-api/internal/search/internal/adapters/deezer"
	"altune/go-api/internal/search/internal/adapters/discogs"
	"altune/go-api/internal/search/internal/adapters/fanarttv"
	"altune/go-api/internal/search/internal/adapters/fansheets"
	"altune/go-api/internal/search/internal/adapters/genius"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/itunes"
	"altune/go-api/internal/search/internal/adapters/musicbrainz"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/adapters/soundcloud"
	"altune/go-api/internal/search/internal/adapters/spotify"
	searchtelemetry "altune/go-api/internal/search/internal/adapters/telemetry"
	"altune/go-api/internal/search/internal/adapters/theaudiodb"
	"altune/go-api/internal/search/internal/adapters/ytmusic"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/queuetimeout"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/telemetry"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"
)

type (
	ClientEventSink             = handler.ClientEventSink
	NoClientEvents              = handler.NoClientEvents
	FeaturedArtist              = domain.FeaturedArtist
	ResultKind                  = domain.ResultKind
	ResultKindSet               = domain.ResultKindSet
	SearchQuery                 = domain.SearchQuery
	SearchResult                = domain.SearchResult
	OwnedTrack                  = ports.OwnedTrack
	OwnedTrackLister            = ports.OwnedTrackLister
	TrackNumberFiller           = ports.TrackNumberFiller
	DeletedUserDataEraser       = ports.DeletedUserDataEraser
	DiscographyQualityReader    = ports.DiscographyQualityReader
	SpellingHintsStore          = ports.SpellingHintsStore
	QueryCount                  = ports.QueryCount
	ChartProvider               = ports.ChartProvider
	ISRCRecording               = ports.ISRCRecording
	MusicBrainzRecording        = ports.MusicBrainzRecording
	MusicBrainzRecordingDetail  = ports.MusicBrainzRecordingDetail
	MusicBrainzRelease          = ports.MusicBrainzRelease
	MusicBrainzWorkRecording    = ports.MusicBrainzWorkRecording
	SearchOutput                = service.SearchOutput
	SearchService               = service.SearchService
	UnreleasedService           = service.UnreleasedService
	UnreleasedConfig            = service.UnreleasedConfig
	SpellingHintsRefreshService = service.SpellingHintsRefreshService
	OwnershipEnrichmentService  = service.OwnershipEnrichmentService
	NoActivityFeed              = service.NoopActivityFeed
	SmokeResult                 = service.SmokeResult
)

const (
	ResultKindAlbum  = domain.ResultKindAlbum
	ResultKindArtist = domain.ResultKindArtist
	ResultKindTrack  = domain.ResultKindTrack
)

var (
	DefaultDiscoveryRateLimits         = handler.DefaultDiscoveryRateLimits
	ErrIdentityStoreUnavailable        = domain.ErrIdentityStoreUnavailable
	NewSearchQuery                     = domain.NewSearchQuery
	ParseDiscographyGroupBy            = ports.ParseDiscographyGroupBy
	NewVocabularyRefreshService        = service.NewVocabularyRefreshService
	NewOwnershipEnrichmentService      = service.NewOwnershipEnrichmentService
	NewOwnershipReader                 = service.NewOwnershipReader
	RunSmokeEval                       = service.RunSmokeEval
	NewPostgresEventStore              = persistence.NewPostgresEventStore
	NewPostgresFavoritesRepository     = persistence.NewPostgresFavoritesRepository
	NewPostgresSearchHistoryRepository = persistence.NewPostgresSearchHistoryRepository
	NewCountingTransport               = searchtelemetry.NewCountingTransport
	ReadProviderMetricsSnapshot        = searchtelemetry.ReadSnapshot
	ProviderRetryRecorder              = searchtelemetry.RetryRecorder
)

type Deps struct {
	Shutdown        <-chan struct{}
	Background      *background.Tracker
	Tracker         *background.Tracker
	Logger          *slog.Logger
	Transport       http.RoundTripper
	ProviderTimeout time.Duration
	ChartTimeout    time.Duration
	Pool            persistence.Database
	Redis           *goredis.Client
	CacheOps        *telemetry.LabeledCounter
	Activity        ports.ActivityFeed
	ClientEvents    handler.ClientEventSink
	Providers       ProviderSettings
	Search          SearchSettings
	Unreleased      UnreleasedSettings
	RateLimits      handler.DiscoveryRateLimits
}

type ProviderSettings struct {
	MusicBrainzUserAgent string
	DiscogsToken         string
	FanartTVAPIKey       string
	GeniusAccessToken    string
	TheAudioDBAPIKey     string
	GoogleSheetsAPIKey   string
	TrackerHubSheetID    string
}

type SearchSettings struct {
	Deadline            time.Duration
	TailDemotion        bool
	CrossKindProminence bool
	Exploration         bool
	ExplorationRate     float64
	BehavioralRanking   bool
	ProviderCacheTTL    time.Duration
}

type UnreleasedSettings struct {
	ServeFromCache bool
	Tuning         service.UnreleasedConfig
}

var (
	errDepsMissing        = errors.New("discovery: deps are missing")
	errProviderTimeoutLow = errors.New("discovery: ProviderTimeout")
)

type requiredHandle struct {
	name    string
	missing bool
}

func (d Deps) requiredHandles() []requiredHandle {
	return []requiredHandle{
		{"Background", d.Background == nil},
		{"Tracker", d.Tracker == nil},
		{"Logger", d.Logger == nil},
		{"Activity", d.Activity == nil},
		{"ClientEvents", d.ClientEvents == nil},
		{"Transport", d.Transport == nil},
		{"CacheOps", d.CacheOps == nil},
		{"ProviderTimeout", d.ProviderTimeout <= 0},
		{"ChartTimeout", d.ChartTimeout <= 0},
	}
}

func (d Deps) validate() error {
	var missing []string
	for _, handle := range d.requiredHandles() {
		if handle.missing {
			missing = append(missing, handle.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w %s", errDepsMissing, strings.Join(missing, ", "))
	}
	if d.ProviderTimeout <= queuetimeout.MaxWait {
		return fmt.Errorf("%w %v must exceed the longest rate-limit queue wait %v", errProviderTimeoutLow, d.ProviderTimeout, queuetimeout.MaxWait)
	}
	return nil
}

type providerClients struct {
	ProviderCalls *http.Client
	ChartCalls    *http.Client
	Transport     http.RoundTripper
}

func newProviderClients(d Deps) providerClients {
	return providerClients{
		ProviderCalls: outbound.MustNewClient(d.ProviderTimeout, d.Transport),
		ChartCalls:    outbound.MustNewClient(d.ChartTimeout, d.Transport),
		Transport:     d.Transport,
	}
}

type storeSet struct {
	Pool          persistence.Database
	Redis         *goredis.Client
	CacheOps      *telemetry.LabeledCounter
	Events        *persistence.PostgresEventStore
	History       *persistence.PostgresSearchHistoryRepository
	SpellingHints ports.SpellingHintsStore
	Identities    ports.IdentityStore
}

func newStoreSet(d Deps) (storeSet, error) {
	var vocabulary ports.SpellingHintsStore
	if d.Redis != nil {
		store, err := newVocabularyStore(d.Redis, d.CacheOps)
		if err != nil {
			return storeSet{}, err
		}
		vocabulary = store
	}
	return storeSet{
		Pool:          d.Pool,
		Redis:         d.Redis,
		CacheOps:      d.CacheOps,
		Events:        persistence.NewPostgresEventStore(d.Pool, clock.Now),
		History:       persistence.NewPostgresSearchHistoryRepository(d.Pool),
		SpellingHints: vocabulary,
		Identities:    identityStoreOf(d),
	}, nil
}

func identityStoreOf(d Deps) ports.IdentityStore {
	if d.Pool == nil {
		return nil
	}
	return cache.NewRedisIdentityStore(persistence.NewPostgresIdentityStore(d.Pool), d.Redis, cacheOption(d.CacheOps))
}

func newVocabularyStore(redis *goredis.Client, cacheOps *telemetry.LabeledCounter) (ports.SpellingHintsStore, error) {
	store, err := cache.NewVocabularyStore(
		redis,
		textnorm.NormalizeForMatch,
		domain.MetaphoneKey,
		cache.WithVocabSignal(cache.NewSignal(cacheOps, clock.Now)),
	)
	if err != nil {
		return nil, fmt.Errorf("build the vocabulary store: %w", err)
	}
	return store, nil
}

func (s storeSet) option() cache.Option {
	return cacheOption(s.CacheOps)
}

func cacheOption(cacheOps *telemetry.LabeledCounter) cache.Option {
	return cache.WithSignal(cache.NewSignal(cacheOps, clock.Now))
}

func (s storeSet) trackerIndex() *cache.RedisTrackerIndexCache {
	return cache.NewRedisTrackerIndexCache(s.Redis, s.option())
}

type recordingAuthority interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]ports.ISRCRecording, error)
	RecordingByMBID(ctx context.Context, mbid string) (ports.MusicBrainzRecording, bool, error)
	WorkRecordings(ctx context.Context, recordingMBID string) ([]ports.MusicBrainzWorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (ports.MusicBrainzRecordingDetail, bool, error)
}

type eventRetention interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

type Module struct {
	handler           *handler.DiscoveryHandler
	Search            *service.SearchService
	FeaturedArtists   *service.FeaturedArtistResolver
	Recordings        recordingAuthority
	Charts            []ports.ChartProvider
	SpellingHints     ports.SpellingHintsStore
	UnreleasedRefresh *service.UnreleasedService
	Events            eventRetention
	rankingOnly       *service.SearchService
}

type wiring struct {
	deps    Deps
	table   *providerTable
	stores  storeSet
	artwork ports.TaggingArtworkLookup
}

func New(ctx context.Context, d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, fmt.Errorf("set up discovery: %w", err)
	}
	w, err := newWiring(ctx, d)
	if err != nil {
		return nil, err
	}
	t, s := w.table, w.stores
	services, err := newHandlerServices(ctx, w)
	if err != nil {
		return nil, err
	}
	unreleasedServices, err := newUnreleasedServices(ctx, d, t, s, services.Artist)
	if err != nil {
		return nil, err
	}
	rankingOnly, err := newRankingOnlySearchService(ctx, w)
	if err != nil {
		return nil, err
	}
	featuredArtists, err := newFeaturedArtists(t)
	if err != nil {
		return nil, fmt.Errorf("build the featured artist resolver: %w", err)
	}
	discoveryHandler, err := newHandler(services, unreleasedServices.lookup)
	if err != nil {
		return nil, err
	}
	return &Module{
		handler:           discoveryHandler,
		Search:            services.Search,
		FeaturedArtists:   featuredArtists,
		Recordings:        t.named(rowMusicBrainz).recordings,
		Charts:            t.chartProviders(),
		SpellingHints:     s.SpellingHints,
		UnreleasedRefresh: unreleasedServices.refresh,
		Events:            s.Events,
		rankingOnly:       rankingOnly,
	}, nil
}

func newWiring(ctx context.Context, d Deps) (wiring, error) {
	s, err := newStoreSet(d)
	if err != nil {
		return wiring{}, fmt.Errorf("build the stores: %w", err)
	}
	t, err := newProviderTable(d.Providers, newProviderClients(d), s)
	if err != nil {
		return wiring{}, fmt.Errorf("build the provider table: %w", err)
	}
	t.log(ctx, d.Logger)
	return wiring{deps: d, table: t, stores: s, artwork: t.artworkChain()}, nil
}

func (m *Module) RankingOnlySearch() *service.SearchService {
	return m.rankingOnly
}

func (m *Module) Routes() handler.Routes { return handler.NewRoutes(m.handler) }

func (m *Module) PublicRoutes(r chi.Router) {
	m.handler.PublicRoutes(r, handler.DefaultAuthFailureLimit)
}

func (m *Module) AttachOwnership(o handler.OwnershipStamper) error {
	if err := m.handler.AttachOwnership(o); err != nil {
		return fmt.Errorf("attach ownership: %w", err)
	}
	return nil
}

func newHandlerServices(ctx context.Context, w wiring) (handler.DiscoveryServices, error) {
	breaker := service.NewCircuitBreaker(clock.Now)
	t, s := w.table, w.stores
	enrichment, err := newEnrichment(t, s, w.artwork)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the enrichment service: %w", err)
	}
	enrichers, err := newDetailEnrichers(t, s)
	if err != nil {
		return handler.DiscoveryServices{}, err
	}
	search, err := newSearchService(ctx, w, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, err
	}
	recordHistory, err := service.NewRecordSearchHistoryService(s.History, clock.Now, ids.Real)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the history recorder: %w", err)
	}
	album, err := newAlbumService(t, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the album tracks service: %w", err)
	}
	artist, err := newArtistService(t, s, breaker, w.deps.Tracker)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the artist content service: %w", err)
	}
	relatedTracks, err := newRelatedService(t, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the related tracks service: %w", err)
	}
	listHistory, err := service.NewListSearchHistoryService(s.History)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the history list service: %w", err)
	}
	clearHistory, err := service.NewClearSearchHistoryService(s.History, clock.Now)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the clear-history service: %w", err)
	}
	favoritesService, err := service.NewFavoritesService(persistence.NewPostgresFavoritesRepository(s.Pool))
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the favorites service: %w", err)
	}
	eventService, err := newEventService(w.deps.Activity, s)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the event service: %w", err)
	}
	return handler.DiscoveryServices{
		Search:        search,
		History:       listHistory,
		RecordHistory: recordHistory,
		ClearHistory:  clearHistory,
		Album:         album,
		Artist:        artist,
		Related:       relatedTracks,
		Enrich:        enrichment,
		Enrichers:     enrichers,
		Ownership:     handler.NoOwnership{},
		ClientEvents:  w.deps.ClientEvents,
		RateLimits:    w.deps.RateLimits,
		Suggest:       service.NewSuggestService(s.SpellingHints),
		Event:         eventService,
		Favorites:     favoritesService,
	}, nil
}

func newEventService(activity ports.ActivityFeed, s storeSet) (*service.RecordEventService, error) {
	r0, err := service.NewRecordEventService(s.Events, activity, clock.Now)
	if err != nil {
		return r0, fmt.Errorf("new record event service: %w", err)
	}
	return r0, nil
}

func newHandler(services handler.DiscoveryServices, unreleasedLookup handler.UnreleasedReader) (*handler.DiscoveryHandler, error) {
	services.Unreleased = unreleasedLookup
	h, err := handler.NewDiscoveryHandler(services, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the discovery handler: %w", err)
	}
	return h, nil
}

type rowName string

const (
	rowDeezer          rowName = "deezer"
	rowAppleMusic      rowName = "applemusic"
	rowMusicBrainz     rowName = "musicbrainz"
	rowSoundCloud      rowName = "soundcloud"
	rowYouTube         rowName = "youtube"
	rowSpotify         rowName = "spotify"
	rowITunes          rowName = "itunes"
	rowCoverArtArchive rowName = "coverartarchive"
	rowDiscogs         rowName = "discogs"
	rowFanartTV        rowName = "fanarttv"
	rowGenius          rowName = "genius"
	rowTheAudioDB      rowName = "theaudiodb"
	rowTrackers        rowName = "trackers"
)

type job string

const (
	jobSearch     job = "search"
	jobArtist     job = "artist"
	jobAlbum      job = "album"
	jobRelated    job = "related"
	jobArtwork    job = "artwork"
	jobCharts     job = "charts"
	jobDetails    job = "details"
	jobEnrichment job = "enrichment"
	jobFeatured   job = "featured"
	jobLyrics     job = "lyrics"
	jobSimilar    job = "similar"
	jobUnreleased job = "unreleased"
	jobRecordings job = "recordings"
)

type trackFeaturedLookup interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

type featuredSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

type similarCatalog interface {
	ports.AlbumContentProvider
	ports.ArtistContentProvider
}

type provider struct {
	name            rowName
	id              domain.ProviderName
	offReason       string
	search          ports.SearchProvider
	searchTimeout   time.Duration
	artist          ports.ArtistContentProvider
	artistAnchor    ports.MusicBrainzDiscographyAnchor
	album           ports.AlbumContentProvider
	albumFallback   ports.SearchProvider
	related         ports.RelatedTracksProvider
	artwork         []ports.ArtworkLookup
	artworkByID     []ports.IdentityArtworkLookup
	charts          ports.ChartProvider
	deezerDetails   ports.DeezerEnricher
	enrichment      ports.DetailsProvider
	trackFeatured   trackFeaturedLookup
	featuredSearch  featuredSearcher
	lyrics          ports.LyricsProvider
	similar         similarCatalog
	unreleasedSongs ports.GeniusSongCatalog
	unreleasedLists ports.TrackerSongs
	recordings      recordingAuthority
}

type providerTable struct {
	providers []provider
}

func newProviderTable(s ProviderSettings, c providerClients, st storeSet) (*providerTable, error) {
	t := &providerTable{}
	t.addDeezer(c)
	t.addAppleMusic(c)
	t.addMusicBrainz(s.MusicBrainzUserAgent, c)
	if err := t.addSoundCloud(c); err != nil {
		return nil, err
	}
	t.addYouTubeMusic(c)
	t.addSpotify(c)
	t.addITunes(c)
	t.addCoverArtArchive(c)
	t.addDiscogs(s.DiscogsToken, s.MusicBrainzUserAgent, c)
	t.addFanartTV(s.FanartTVAPIKey, c)
	t.addGenius(s.GeniusAccessToken, c)
	t.addTheAudioDB(s.TheAudioDBAPIKey, c)
	if err := t.addTrackers(s.GoogleSheetsAPIKey, s.TrackerHubSheetID, c, st); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *providerTable) add(p provider) {
	t.providers = append(t.providers, p)
}

func switchedOff(name rowName, reason string) provider {
	return provider{name: name, offReason: reason}
}

func (t *providerTable) named(name rowName) provider {
	for _, p := range t.providers {
		if p.name == name {
			return p
		}
	}
	return provider{}
}

type jobSlot struct {
	job    job
	filled bool
}

func (p provider) catalogSlots() []jobSlot {
	return []jobSlot{
		{jobSearch, p.search != nil},
		{jobArtist, p.artist != nil},
		{jobArtist, p.artistAnchor != nil},
		{jobAlbum, p.album != nil},
		{jobAlbum, p.albumFallback != nil},
		{jobRelated, p.related != nil},
		{jobArtwork, len(p.artwork)+len(p.artworkByID) > 0},
		{jobCharts, p.charts != nil},
	}
}

func (p provider) detailSlots() []jobSlot {
	return []jobSlot{
		{jobDetails, p.deezerDetails != nil},
		{jobEnrichment, p.enrichment != nil},
		{jobFeatured, p.trackFeatured != nil},
		{jobFeatured, p.featuredSearch != nil},
		{jobLyrics, p.lyrics != nil},
		{jobSimilar, p.similar != nil},
		{jobUnreleased, p.unreleasedSongs != nil},
		{jobUnreleased, p.unreleasedLists != nil},
		{jobRecordings, p.recordings != nil},
	}
}

func (p provider) jobs() []job {
	var jobs []job
	for _, slot := range append(p.catalogSlots(), p.detailSlots()...) {
		if slot.filled && !slices.Contains(jobs, slot.job) {
			jobs = append(jobs, slot.job)
		}
	}
	return jobs
}

var artworkAskingOrder = []rowName{
	rowCoverArtArchive, rowSpotify, rowDiscogs, rowFanartTV, rowGenius, rowTheAudioDB,
	rowDeezer, rowITunes, rowYouTube, rowSoundCloud,
}

func (t *providerTable) searchProviders() []ports.SearchProvider {
	var searchers []ports.SearchProvider
	for _, p := range t.providers {
		if p.search != nil {
			searchers = append(searchers, p.search)
		}
	}
	return searchers
}

func (t *providerTable) searchTimeouts() map[domain.ProviderName]time.Duration {
	timeouts := map[domain.ProviderName]time.Duration{}
	for _, p := range t.providers {
		if p.search != nil && p.searchTimeout > 0 {
			timeouts[p.search.Name()] = p.searchTimeout
		}
	}
	return timeouts
}

func (t *providerTable) artistProviders() map[domain.ProviderName]ports.ArtistContentProvider {
	artists := map[domain.ProviderName]ports.ArtistContentProvider{}
	for _, p := range t.providers {
		if p.artist != nil {
			artists[p.id] = p.artist
		}
	}
	return artists
}

func (t *providerTable) albumProviders() map[domain.ProviderName]ports.AlbumContentProvider {
	albums := map[domain.ProviderName]ports.AlbumContentProvider{}
	for _, p := range t.providers {
		if p.album != nil {
			albums[p.id] = p.album
		}
	}
	return albums
}

func (t *providerTable) relatedProviders() map[string]ports.RelatedTracksProvider {
	related := map[string]ports.RelatedTracksProvider{}
	for _, p := range t.providers {
		if p.related != nil {
			related[p.id.String()] = p.related
		}
	}
	return related
}

func (t *providerTable) chartProviders() []ports.ChartProvider {
	var charts []ports.ChartProvider
	for _, p := range t.providers {
		if p.charts != nil {
			charts = append(charts, p.charts)
		}
	}
	return charts
}

func (t *providerTable) artworkChain() *artworkfallback.ChainedArtworkLookup {
	var resolvers []ports.ArtworkLookup
	var byIdentity []ports.IdentityArtworkLookup
	for _, name := range artworkAskingOrder {
		row := t.named(name)
		resolvers = append(resolvers, row.artwork...)
		byIdentity = append(byIdentity, row.artworkByID...)
	}
	return artworkfallback.NewChainedArtworkLookupWithIdentities(byIdentity, resolvers...)
}

func (t *providerTable) log(ctx context.Context, logger *slog.Logger) {
	for _, p := range t.providers {
		logger.InfoContext(ctx, "discovery.provider", p.logAttrs()...)
	}
}

func (p provider) logAttrs() []any {
	if p.offReason != "" {
		return []any{"name", string(p.name), "on", false, "reason", p.offReason}
	}
	return []any{"name", string(p.name), "on", true, "jobs", joinJobs(p.jobs())}
}

func joinJobs(jobs []job) string {
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = string(j)
	}
	return strings.Join(names, ",")
}

func (t *providerTable) addDeezer(c providerClients) {
	adapter := deezer.NewDeezerAdapter(c.ProviderCalls)
	t.add(provider{
		name:          rowDeezer,
		id:            domain.ProviderDeezer,
		search:        adapter,
		artist:        adapter,
		album:         adapter,
		albumFallback: adapter,
		artwork:       []ports.ArtworkLookup{adapter},
		charts:        deezer.NewDeezerAdapter(c.ChartCalls),
		deezerDetails: adapter,
		trackFeatured: adapter,
		lyrics:        deezer.NewDeezerLyricsAdapter(c.ProviderCalls, clock.Now),
		similar:       adapter,
	})
}

func (t *providerTable) addAppleMusic(c providerClients) {
	adapter := applemusic.NewAppleMusicAdapter(c.ProviderCalls, clock.Now)
	t.add(provider{name: rowAppleMusic, id: domain.ProviderAppleMusic, search: adapter, searchTimeout: adapter.SearchTimeout(), artist: adapter, album: adapter})
}

func (t *providerTable) addMusicBrainz(userAgent string, c providerClients) {
	if userAgent == "" {
		t.add(switchedOff(rowMusicBrainz, "MUSICBRAINZ_USER_AGENT is empty"))
		return
	}
	adapter := musicbrainz.NewMusicBrainzAdapter(c.ProviderCalls, userAgent, clock.Now)
	t.add(provider{
		name:           rowMusicBrainz,
		id:             domain.ProviderMusicBrainz,
		artistAnchor:   adapter,
		enrichment:     adapter,
		featuredSearch: adapter,
		recordings:     adapter,
	})
}

func (t *providerTable) addSoundCloud(c providerClients) error {
	adapter, err := soundcloud.NewSoundCloudAPIAdapter(c.ProviderCalls, soundcloud.NoFallback{}, clock.Now)
	if err != nil {
		return fmt.Errorf("build the soundcloud content adapter: %w", err)
	}
	searcher, err := soundcloud.NewSoundCloudAPIAdapter(c.ProviderCalls, soundcloud.NewSoundCloudAdapter(), clock.Now)
	if err != nil {
		return fmt.Errorf("build the soundcloud search adapter: %w", err)
	}
	t.add(provider{
		name:          rowSoundCloud,
		id:            domain.ProviderSoundCloud,
		search:        searcher,
		searchTimeout: searcher.SearchTimeout(),
		artist:        adapter,
		album:         adapter,
		related:       adapter,
		artwork:       []ports.ArtworkLookup{adapter},
	})
	return nil
}

func (t *providerTable) addYouTubeMusic(c providerClients) {
	adapter := ytmusic.MustNewYouTubeMusicAdapter(c.Transport, clock.Sleep, random.Real)
	t.add(provider{
		name:          rowYouTube,
		id:            domain.ProviderYouTube,
		search:        adapter,
		searchTimeout: adapter.SearchTimeout(),
		artwork:       []ports.ArtworkLookup{ytmusic.MustNewYouTubeMusicArtworkLookup(c.Transport, clock.Sleep, random.Real)},
	})
}

func (t *providerTable) addSpotify(c providerClients) {
	adapter := spotify.NewSpotifyAdapter(c.ProviderCalls, clock.Now)
	artwork := spotify.NewSpotifyArtworkLookup(c.ProviderCalls)
	t.add(provider{
		name:          rowSpotify,
		id:            domain.ProviderSpotify,
		search:        adapter,
		searchTimeout: adapter.SearchTimeout(),
		artist:        adapter,
		album:         adapter,
		artworkByID:   []ports.IdentityArtworkLookup{artwork},
	})
}

func (t *providerTable) addITunes(c providerClients) {
	adapter := itunes.NewITunesAdapter(c.ProviderCalls)
	t.add(provider{
		name:    rowITunes,
		id:      domain.ProviderITunes,
		album:   adapter,
		artwork: []ports.ArtworkLookup{adapter},
	})
}

func (t *providerTable) addCoverArtArchive(c providerClients) {
	byTitle := coverartarchive.NewCoverArtArchiveResolver(c.ProviderCalls)
	byIdentity := coverartarchive.NewCoverArtArchiveIdentityResolver(byTitle)
	t.add(provider{name: rowCoverArtArchive, artwork: []ports.ArtworkLookup{byTitle}, artworkByID: []ports.IdentityArtworkLookup{byIdentity}})
}

func (t *providerTable) addDiscogs(token, userAgent string, c providerClients) {
	if token == "" {
		t.add(switchedOff(rowDiscogs, "DISCOGS_TOKEN is empty"))
		return
	}
	adapter := discogs.NewDiscogsAdapter(c.ProviderCalls, token, userAgent)
	t.add(provider{
		name:        rowDiscogs,
		id:          domain.ProviderDiscogs,
		artworkByID: []ports.IdentityArtworkLookup{adapter},
	})
}

func (t *providerTable) addFanartTV(apiKey string, c providerClients) {
	if apiKey == "" {
		t.add(switchedOff(rowFanartTV, "FANARTTV_API_KEY is empty"))
		return
	}
	resolver := fanarttv.NewFanartTvArtworkLookup(c.ProviderCalls, apiKey)
	t.add(provider{name: rowFanartTV, artwork: []ports.ArtworkLookup{resolver}})
}

func (t *providerTable) addGenius(accessToken string, c providerClients) {
	if accessToken == "" {
		t.add(switchedOff(rowGenius, "GENIUS_ACCESS_TOKEN is empty"))
		return
	}
	t.add(provider{
		name:            rowGenius,
		artwork:         []ports.ArtworkLookup{genius.NewGeniusArtworkLookup(c.ProviderCalls, accessToken)},
		unreleasedSongs: genius.NewSongCatalog(c.ProviderCalls, accessToken),
	})
}

func (t *providerTable) addTheAudioDB(apiKey string, c providerClients) {
	if apiKey == "" {
		t.add(switchedOff(rowTheAudioDB, "THEAUDIODB_API_KEY is empty"))
		return
	}
	adapter := theaudiodb.New(c.ProviderCalls, theaudiodb.WithAPIKey(apiKey))
	t.add(provider{name: rowTheAudioDB, id: domain.ProviderTheAudioDB, artwork: []ports.ArtworkLookup{adapter}})
}

func (t *providerTable) addTrackers(apiKey, hubSheetID string, c providerClients, st storeSet) error {
	if apiKey == "" {
		t.add(switchedOff(rowTrackers, "GOOGLE_SHEETS_API_KEY is empty"))
		return nil
	}
	client, err := fansheets.NewClient(c.ProviderCalls, apiKey, hubSheetID, st.trackerIndex())
	if err != nil {
		return fmt.Errorf("build the tracker sheet client: %w", err)
	}
	t.add(provider{name: rowTrackers, unreleasedLists: client})
	return nil
}

func newAlbumService(t *providerTable, breaker *service.CircuitBreaker) (*service.GetAlbumTracksService, error) {
	deezerRow := t.named(rowDeezer)
	featured, err := albumFeaturedEnrichment(deezerRow, breaker)
	if err != nil {
		return nil, err
	}
	fallback, err := albumFallback(deezerRow, breaker)
	if err != nil {
		return nil, err
	}
	r0, err := service.NewGetAlbumTracksService(t.albumProviders(), featured, fallback, breaker)
	if err != nil {
		return r0, fmt.Errorf("new get album tracks service: %w", err)
	}
	return r0, nil
}

func albumFeaturedEnrichment(row provider, breaker *service.CircuitBreaker) (service.FeaturedEnrichment, error) {
	if row.trackFeatured == nil {
		return service.NoFeaturedEnrichment{}, nil
	}
	r0, err := service.NewDeezerFeaturedEnrichment(row.trackFeatured, breaker)
	if err != nil {
		return r0, fmt.Errorf("new deezer featured enrichment: %w", err)
	}
	return r0, nil
}

func albumFallback(row provider, breaker *service.CircuitBreaker) (service.AlbumFallback, error) {
	if row.albumFallback == nil {
		return service.NoAlbumFallback{}, nil
	}
	r0, err := service.NewSearchAlbumFallback(row.albumFallback, breaker)
	if err != nil {
		return r0, fmt.Errorf("new search album fallback: %w", err)
	}
	return r0, nil
}

func newRelatedService(t *providerTable, breaker *service.CircuitBreaker) (*service.GetRelatedTracksService, error) {
	r0, err := service.NewGetRelatedTracksService(t.relatedProviders(), breaker)
	if err != nil {
		return r0, fmt.Errorf("new get related tracks service: %w", err)
	}
	return r0, nil
}

func newArtistService(t *providerTable, s storeSet, breaker *service.CircuitBreaker, bg *background.Tracker) (*service.GetArtistContentService, error) {
	identity, err := artistIdentity(s)
	if err != nil {
		return nil, err
	}
	anchoring, err := artistAnchoring(t.named(rowMusicBrainz), breaker)
	if err != nil {
		return nil, err
	}
	discographyTelemetry, err := service.NewDiscographyTelemetry(s.Events, bg, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the discography telemetry: %w", err)
	}
	r0, err := service.NewGetArtistContentService(t.artistProviders(), t.albumProviders(), identity, anchoring, discographyTelemetry, breaker)
	if err != nil {
		return r0, fmt.Errorf("new get artist content service: %w", err)
	}
	return r0, nil
}

func artistIdentity(s storeSet) (service.ArtistIdentity, error) {
	if s.Identities == nil {
		return service.SeedPathIdentity{}, nil
	}
	r0, err := service.NewStoreIdentity(s.Identities)
	if err != nil {
		return r0, fmt.Errorf("new store identity: %w", err)
	}
	return r0, nil
}

func artistAnchoring(musicBrainz provider, breaker *service.CircuitBreaker) (service.Anchoring, error) {
	if musicBrainz.artistAnchor == nil {
		return service.NoAnchoring{}, nil
	}
	r0, err := service.NewMusicBrainzAnchoring(musicBrainz.artistAnchor, breaker)
	if err != nil {
		return r0, fmt.Errorf("new music brainz anchoring: %w", err)
	}
	return r0, nil
}

func newFeaturedArtists(t *providerTable) (*service.FeaturedArtistResolver, error) {
	var mb service.MBFeaturedSearcher = service.NoMBFeatured{}
	if searcher := t.named(rowMusicBrainz).featuredSearch; searcher != nil {
		mb = searcher
	}
	var deezerLookup service.DeezerFeaturedLookup = service.NoDeezerFeatured{}
	if lookup := t.named(rowDeezer).trackFeatured; lookup != nil {
		deezerLookup = lookup
	}
	r0, err := service.NewFeaturedArtistResolver(mb, deezerLookup)
	if err != nil {
		return r0, fmt.Errorf("new featured artist resolver: %w", err)
	}
	return r0, nil
}

func newEnrichment(t *providerTable, s storeSet, artwork ports.TaggingArtworkLookup) (handler.Enricher, error) {
	enricher := t.named(rowMusicBrainz).enrichment
	if enricher == nil {
		return handler.UnconfiguredEnrichment{}, nil
	}
	enrichmentCache := cache.NewRedisEnrichmentCache(s.Redis, s.option())
	r0, err := service.NewEnrichmentService(enricher, artwork, enrichmentCache, enrichmentCache)
	if err != nil {
		return r0, fmt.Errorf("new enrichment service: %w", err)
	}
	return r0, nil
}

func newDetailEnrichers(t *providerTable, s storeSet) (handler.DetailEnrichers, error) {
	deezerRow := t.named(rowDeezer)
	deezerEnrichment, err := service.NewDeezerEnrichmentService(deezerRow.deezerDetails, cache.NewRedisDeezerEnrichmentCache(s.Redis, s.option()))
	if err != nil {
		return handler.DetailEnrichers{}, fmt.Errorf("build the deezer enrichment service: %w", err)
	}
	lyrics, err := service.NewLyricsService(deezerRow.lyrics, cache.NewRedisDeezerLyricsCache(s.Redis, s.option()))
	if err != nil {
		return handler.DetailEnrichers{}, fmt.Errorf("build the lyrics service: %w", err)
	}
	return handler.DetailEnrichers{Deezer: deezerEnrichment, Lyrics: lyrics}, nil
}

type searchPlan struct {
	collaborators service.Collaborators
	options       []service.Option
	chosen        []string
}

func newSearchPlan() *searchPlan {
	return &searchPlan{collaborators: service.AbsentCollaborators()}
}

func (p *searchPlan) choose(name string) {
	p.chosen = append(p.chosen, name)
}

func (p *searchPlan) option(name string, opt service.Option) {
	p.options = append(p.options, opt)
	p.choose(name)
}

func newSearchService(ctx context.Context, w wiring, breaker *service.CircuitBreaker) (*service.SearchService, error) {
	p := newSearchPlan()
	planRanking(p, w.deps.Search, w.stores)
	planExplorationAndDeadline(p, w.deps.Search)
	if err := planArtworkAndRelated(p, w, breaker); err != nil {
		return nil, err
	}
	planIdentityAndFavorites(p, w.stores)
	planResultCaches(p, w.deps.Search, w.stores)
	planEnrichmentCaches(p, w.stores)
	planVocabulary(p, w.stores)
	if err := planRecording(p, w); err != nil {
		return nil, err
	}
	return w.namedSearchService(ctx, "discovery.search", p, breaker)
}

func newRankingOnlySearchService(ctx context.Context, w wiring) (*service.SearchService, error) {
	p := newSearchPlan()
	planRanking(p, w.deps.Search, w.stores)
	planVocabulary(p, w.stores)
	return w.namedSearchService(ctx, "discovery.ranking_only_search", p, service.NewCircuitBreaker(clock.Now))
}

func (w wiring) namedSearchService(ctx context.Context, message string, p *searchPlan, breaker *service.CircuitBreaker) (*service.SearchService, error) {
	w.deps.Logger.InfoContext(ctx, message, "options", strings.Join(p.chosen, ","))
	options := append([]service.Option{service.WithProviderTimeouts(w.table.searchTimeouts())}, p.options...)
	svc, err := service.NewSearchService(w.deps.Tracker, w.table.searchProviders(), breaker, p.collaborators, options...)
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", message, err)
	}
	return svc, nil
}

func planRanking(p *searchPlan, settings SearchSettings, s storeSet) {
	p.collaborators.History = s.History
	p.choose("history")
	if settings.TailDemotion {
		p.option("tail_demotion", service.WithTailDemotion())
	}
	if settings.CrossKindProminence {
		p.option("cross_kind_prominence", service.WithCrossKindProminence())
	}
}

func planExplorationAndDeadline(p *searchPlan, settings SearchSettings) {
	if settings.Exploration && settings.ExplorationRate > 0 {
		p.option("exploration", service.WithExploration(settings.ExplorationRate))
	}
	if settings.Deadline > 0 {
		p.option("deadline", service.WithSearchDeadline(settings.Deadline))
	}
}

func planArtworkAndRelated(p *searchPlan, w wiring, breaker *service.CircuitBreaker) error {
	filler, err := newArtworkFiller(w)
	if err != nil {
		return fmt.Errorf("build the artwork filler: %w", err)
	}
	p.collaborators.Artwork = filler
	p.choose("artwork")

	findRelated, err := newFindRelatedService(w.table, w.stores, breaker)
	if err != nil {
		return fmt.Errorf("build the find related service: %w", err)
	}
	p.collaborators.Related = findRelated
	p.choose("find_related")
	return nil
}

func newFindRelatedService(t *providerTable, s storeSet, breaker *service.CircuitBreaker) (*service.FindRelatedService, error) {
	similar := t.named(rowDeezer).similar
	library, err := service.LibraryMatches(persistence.NewPostgresRelationshipQuerier(s.Pool))
	if err != nil {
		return nil, fmt.Errorf("build the library matches source: %w", err)
	}
	albumTracks, err := service.AlbumTracksSource(similar, breaker)
	if err != nil {
		return nil, fmt.Errorf("build the album tracks source: %w", err)
	}
	artistAlbums, err := service.ArtistAlbumsSource(similar, breaker)
	if err != nil {
		return nil, fmt.Errorf("build the artist albums source: %w", err)
	}
	r0, err := service.NewFindRelatedService(clock.Now, library, albumTracks, artistAlbums)
	if err != nil {
		return r0, fmt.Errorf("new find related service: %w", err)
	}
	return r0, nil
}

func newArtworkFiller(w wiring) (*service.ArtworkFiller, error) {
	s := w.stores
	var identities ports.IdentityStore = service.NoIdentities{}
	if s.Identities != nil {
		identities = s.Identities
	}
	if s.Redis == nil {
		r0, err := service.NewPendingFiller(identities, w.deps.Tracker)
		if err != nil {
			return r0, fmt.Errorf("new pending filler: %w", err)
		}
		return r0, nil
	}
	r0, err := service.NewArtworkFiller(
		w.artwork,
		cache.NewRedisArtworkCache(s.Redis, s.option()),
		identities,
		cache.NewRedisEnrichmentCache(s.Redis, s.option()),
		w.deps.Tracker,
	)
	if err != nil {
		return r0, fmt.Errorf("new artwork filler: %w", err)
	}
	return r0, nil
}

func planIdentityAndFavorites(p *searchPlan, s storeSet) {
	if s.Pool != nil {
		p.collaborators.Favorites = persistence.NewPostgresFavoritesRepository(s.Pool)
		p.choose("favorites")
	}
	if s.Identities != nil {
		p.choose("identity_store")
	}
}

func planResultCaches(p *searchPlan, settings SearchSettings, s storeSet) {
	if s.Redis == nil {
		return
	}
	p.collaborators.ResultCache = cache.NewRedisResultCache(s.Redis, s.option())
	p.choose("result_cache")
	p.collaborators.ProviderCache = cache.NewRedisProviderResultCache(s.Redis, settings.ProviderCacheTTL, s.option())
	p.choose("provider_result_cache")
	p.collaborators.HeldSlates = cache.NewRedisHeldSlateCache(s.Redis, s.option())
	p.choose("held_slate_cache")
}

func planEnrichmentCaches(p *searchPlan, s storeSet) {
	if s.Redis == nil {
		return
	}
	p.choose("artwork_cache")
	p.choose("mbid_index")
}

func planVocabulary(p *searchPlan, s storeSet) {
	if s.SpellingHints == nil {
		return
	}
	p.collaborators.Vocabulary = s.SpellingHints
	p.choose("vocabulary")
}

func planRecording(p *searchPlan, w wiring) error {
	p.collaborators.Events = w.stores.Events
	p.choose("event_store")
	if w.deps.Search.BehavioralRanking {
		scoring, err := service.NewBehavioralScoring(service.NewSatisfactionConsumer(w.stores.Events), clock.Now)
		if err != nil {
			return fmt.Errorf("build behavioral ranking: %w", err)
		}
		p.collaborators.Behavior = scoring
		p.choose("behavioral_ranking")
	}
	p.collaborators.Activity = w.deps.Activity
	p.choose("activity")
	return nil
}

type unreleasedServices struct {
	lookup  handler.UnreleasedReader
	refresh *service.UnreleasedService
}

func newUnreleasedServices(ctx context.Context, d Deps, t *providerTable, s storeSet, artists *service.GetArtistContentService) (unreleasedServices, error) {
	songs, lists := t.named(rowGenius).unreleasedSongs, t.named(rowTrackers).unreleasedLists
	if songs == nil && lists == nil {
		return unreleasedServices{lookup: handler.NoUnreleased{}}, nil
	}
	var geniusSongs ports.GeniusSongCatalog = service.NoGenius{}
	if songs != nil {
		geniusSongs = songs
	}
	var trackers ports.TrackerSongs = service.NoTrackers{}
	if lists != nil {
		trackers = lists
	}
	storage, backend, refreshes, err := unreleasedStorage(ctx, d, s)
	if err != nil {
		return unreleasedServices{}, err
	}
	lookup, err := service.NewUnreleasedService(geniusSongs, artists, d.Unreleased.Tuning, service.Wiring{
		Trackers: trackers,
		Storage:  storage,
		Backend:  backend,
		Shutdown: d.Shutdown,
		Jobs:     d.Background,
		Timer:    clock.NewTimer,
		Now:      clock.Now,
	})
	if err != nil {
		return unreleasedServices{}, fmt.Errorf("build the unreleased service: %w", err)
	}
	if refreshes {
		return unreleasedServices{lookup: lookup, refresh: lookup}, nil
	}
	return unreleasedServices{lookup: lookup}, nil
}

func unreleasedStorage(ctx context.Context, d Deps, s storeSet) (service.Storage, service.Background, bool, error) {
	if store := unreleasedStore(ctx, d, s); store != nil {
		storage, err := service.NewStoredStorage(store)
		if err != nil {
			return nil, nil, false, fmt.Errorf("build the unreleased store storage: %w", err)
		}
		return storage, storage, true, nil
	}
	storage, err := service.NewCachedStorage(cache.NewRedisUnreleasedCache(s.Redis, s.option()))
	if err != nil {
		return nil, nil, false, fmt.Errorf("build the unreleased cache storage: %w", err)
	}
	return storage, service.NoBackground{}, false, nil
}

func unreleasedStore(ctx context.Context, d Deps, s storeSet) ports.UnreleasedStore {
	if d.Unreleased.ServeFromCache {
		return nil
	}
	if s.Pool == nil {
		d.Logger.WarnContext(ctx, "unreleased.store_unavailable_using_cache", "reason", "no database pool")
		return nil
	}
	return persistence.NewPostgresUnreleasedStore(s.Pool)
}
