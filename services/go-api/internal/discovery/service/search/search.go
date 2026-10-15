package search

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/artwork"
	"altune/go-api/internal/discovery/service/favorites"
	"altune/go-api/internal/discovery/service/history"
	"altune/go-api/internal/discovery/service/identity"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/rank"
	"altune/go-api/internal/discovery/service/related"
	"altune/go-api/internal/discovery/service/vocabulary"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/runloop"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"runtime/debug"
	"slices"
	"time"
)

type Service struct {
	providers      []ports.SearchProvider
	circuitBreaker *providercall.CircuitBreaker
	providerCache  ports.ProviderResultCache
	deadline       time.Duration

	identity       *identity.IdentityStamper
	artwork        *artwork.ArtworkFiller
	ranking        *rank.RankingExperiments
	favorites      *favorites.FavoritesLifter
	correctionSvc  *vocabulary.CorrectionService
	findRelatedSvc *related.FindRelatedService
	cache          *searchResultCache
	history        *history.RecordSearchHistoryService
	telemetry      *SearchTelemetry
	vocab          *vocabulary.VocabularyIngestor

	bg *runloop.Tracker

	resolver *sharedResolver
}

type SearchOutput struct {
	SearchId         string
	QueryNorm        string
	Explored         bool
	Results          []domain.SearchResult
	ProviderStatuses []domain.ProviderSearchResponse
	Partial          bool
	CorrectedQuery   string
	OriginalQuery    string
	Related          []domain.RelatedGroup
	Total            int
	Offset           int
	HasMore          bool
	Cached           bool
	Slate            BlendedSlate
}

type serviceConfig struct {
	historyRepo    ports.HistoryWriter
	vocabStore     ports.VocabularyStore
	eventStore     ports.EventStore
	activityFeed   ports.ActivityFeed
	resultCache    ports.ResultCache
	providerCache  ports.ProviderResultCache
	heldSlateCache ports.HeldSlateCache
	favoritesRepo  ports.FavoritesRepository

	artworkResolver  ports.TaggingArtworkResolver
	artworkCache     ports.ArtworkCache
	identityBridge   ports.IdentityBridge
	mbidIndex        ports.MBIDIndex
	identityStore    ports.IdentityStore
	identityVerifier *identity.IdentityVerifier

	findRelatedSvc *related.FindRelatedService

	searchDeadline time.Duration

	ranking rank.RankingConfig
}

type Option func(*serviceConfig)

func WithHistoryRepository(r ports.HistoryWriter) Option {
	return func(c *serviceConfig) { c.historyRepo = r }
}

func WithVocabularyStore(v ports.VocabularyStore) Option {
	return func(c *serviceConfig) { c.vocabStore = v }
}

func WithEventStore(e ports.EventStore) Option {
	return func(c *serviceConfig) { c.eventStore = e }
}

func WithSearchActivityFeed(activity ports.ActivityFeed) Option {
	return func(c *serviceConfig) { c.activityFeed = activity }
}

func WithArtworkResolver(r ports.TaggingArtworkResolver) Option {
	return func(c *serviceConfig) { c.artworkResolver = r }
}

func WithArtworkCache(ac ports.ArtworkCache) Option {
	return func(c *serviceConfig) { c.artworkCache = ac }
}

func WithIdentityBridge(b ports.IdentityBridge) Option {
	return func(c *serviceConfig) { c.identityBridge = b }
}

func WithMBIDIndex(idx ports.MBIDIndex) Option {
	return func(c *serviceConfig) { c.mbidIndex = idx }
}

func WithIdentityStore(store ports.IdentityStore) Option {
	return func(c *serviceConfig) { c.identityStore = store }
}

func WithIdentityVerifier(v *identity.IdentityVerifier) Option {
	return func(c *serviceConfig) { c.identityVerifier = v }
}

func WithFindRelatedService(r *related.FindRelatedService) Option {
	return func(c *serviceConfig) { c.findRelatedSvc = r }
}

func WithResultCache(rc ports.ResultCache) Option {
	return func(c *serviceConfig) { c.resultCache = rc }
}

func WithProviderResultCache(pc ports.ProviderResultCache) Option {
	return func(c *serviceConfig) { c.providerCache = pc }
}

func WithSearchDeadline(d time.Duration) Option {
	return func(c *serviceConfig) { c.searchDeadline = d }
}

func WithHeldSlateCache(hc ports.HeldSlateCache) Option {
	return func(c *serviceConfig) { c.heldSlateCache = hc }
}

func WithFavorites(repo ports.FavoritesRepository) Option {
	return func(c *serviceConfig) { c.favoritesRepo = repo }
}

func WithTailDemotion() Option {
	return func(c *serviceConfig) { c.ranking.TailDemotion = true }
}

func WithCrossKindProminence() Option {
	return func(c *serviceConfig) { c.ranking.CrossKindProminence = true }
}

func WithBehavioralRanking(consumer *rank.SatisfactionConsumer) Option {
	return func(c *serviceConfig) {
		c.ranking.BehavioralRanking = true
		c.ranking.BehavioralConsumer = consumer
	}
}

func WithExploration(rate float64) Option {
	return func(c *serviceConfig) {
		if rate > 0 {
			c.ranking.ExplorationRate = rate
		}
	}
}

func (s *Service) MaybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	return s.ranking.MaybeExplore(ranked)
}

func (s *Service) CircuitBreaker() *providercall.CircuitBreaker {
	return s.circuitBreaker
}

func NewService(providers []ports.SearchProvider, circuitBreaker *providercall.CircuitBreaker, opts ...Option) *Service {
	var cfg serviceConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	bg := &runloop.Tracker{}
	s := assembleService(providers, circuitBreaker, cfg, bg)
	if cfg.vocabStore != nil {
		s.correctionSvc = vocabulary.NewCorrectionService(cfg.vocabStore)
	}
	return s
}

func (c serviceConfig) heldSlate() ports.HeldSlateCache {
	if c.heldSlateCache == nil {
		return c.resultCache
	}
	return c.heldSlateCache
}

func assembleService(providers []ports.SearchProvider, circuitBreaker *providercall.CircuitBreaker, cfg serviceConfig, bg *runloop.Tracker) *Service {
	return &Service{
		providers:      providers,
		circuitBreaker: circuitBreaker,
		providerCache:  cfg.providerCache,
		deadline:       cfg.searchDeadline,
		identity:       identity.NewIdentityStamper(cfg.identityBridge, cfg.identityStore, cfg.identityVerifier, bg),
		artwork:        artwork.NewArtworkFiller(cfg.artworkResolver, cfg.artworkCache, cfg.identityStore, cfg.mbidIndex),
		ranking:        rank.NewRankingExperiments(cfg.ranking, bg),
		favorites:      favorites.NewFavoritesLifter(cfg.favoritesRepo),
		findRelatedSvc: cfg.findRelatedSvc,
		cache:          newSearchResultCache(cfg.resultCache, cfg.heldSlate()),
		history:        history.NewRecordSearchHistoryService(cfg.historyRepo),
		telemetry:      newSearchTelemetry(cfg.eventStore, cfg.activityFeed, bg),
		vocab:          vocabulary.NewVocabularyIngestor(cfg.vocabStore, bg),
		bg:             bg,
		resolver:       newSharedResolver(),
	}
}

type searchRun struct {
	searchId    string
	userId      shared.UserId
	query       *domain.SearchQuery
	queryNorm   string
	saveHistory bool
	startedAt   time.Time
}

func (s *Service) Execute(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	saveHistory bool,
) (*SearchOutput, error) {
	return s.ExecutePage(ctx, userId, query, saveHistory, shared.EntityId{})
}

func (s *Service) ExecutePage(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	saveHistory bool,
	continues shared.EntityId,
) (*SearchOutput, error) {
	startedAt := time.Now().UTC()
	searchQuery := domain.SearchText(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)

	slog.InfoContext(ctx, "search.v2.start", logging.SearchTextAttr(query.Raw))

	resolution, searchId := s.slateForPage(ctx, query, searchQuery, queryNorm, continues)
	run := searchRun{
		searchId:    searchId.String(),
		userId:      userId,
		query:       query,
		queryNorm:   queryNorm,
		saveHistory: saveHistory,
		startedAt:   startedAt,
	}
	stages := resolution.stages
	favoritesDone := timeStage(&stages.favorites)
	ranked := s.favorites.Lift(ctx, userId, resolution.ranked)
	favoritesDone()

	var related []domain.RelatedGroup
	relatedDone := timeStage(&stages.related)
	if s.findRelatedSvc != nil && len(ranked) > 0 {
		related = s.findRelatedSvc.Execute(ctx, userId, ranked)
	}
	relatedDone()

	total := len(ranked)
	fullSlate := ranked
	organic := pageOf(ranked, query.Offset, query.Limit)
	hasMore := query.Offset+len(organic) < total

	if hasMore && searchId != continues {
		s.cache.holdSlate(ctx, searchId, queryNorm, query.Kinds, resolution.ranked)
	}

	shown := organic
	explored := false
	var slate BlendedSlate
	if query.Offset == 0 {
		shown, explored = s.MaybeExplore(organic)
		slate = BuildBlendedSlate(shown, fullSlate)
		s.recordFirstPageSideEffects(ctx, run, firstPage{
			shown:     shown,
			organic:   organic,
			fullSlate: fullSlate,
			related:   related,
			explored:  explored,
			timing:    searchTiming{stages: stages, total: time.Since(startedAt), cached: resolution.cached, partial: resolution.partial},
		})
	}

	slog.InfoContext(ctx, "search.v2.complete", append([]any{
		logging.SearchTextAttr(query.Raw),
		"results", len(shown),
		"partial", resolution.partial,
		"corrected", resolution.correctedQuery != "",
		"related_groups", len(related),
		"cached", resolution.cached,
		"continued", searchId == continues,
		"offset", query.Offset,
		"total", total,
		"tail_noise_top5", rank.TailNoiseInTopK(shown, 5),
	}, stages.logAttrs(time.Since(startedAt))...)...)

	return &SearchOutput{
		SearchId:         run.searchId,
		QueryNorm:        run.queryNorm,
		Explored:         explored,
		Results:          shown,
		Total:            total,
		Offset:           query.Offset,
		HasMore:          hasMore,
		Slate:            slate,
		ProviderStatuses: resolution.statuses,
		Partial:          resolution.partial,
		CorrectedQuery:   resolution.correctedQuery,
		OriginalQuery:    resolution.originalQuery,
		Related:          related,
		Cached:           resolution.cached,
	}, nil
}

type rankedResolution struct {
	ranked         []domain.SearchResult
	statuses       []domain.ProviderSearchResponse
	correctedQuery string
	originalQuery  string
	partial        bool
	pending        bool
	cached         bool
	stages         stageTimings
}

func (r rankedResolution) isAuthoritative() bool {
	return len(r.ranked) > 0 && !r.partial && !r.pending && r.correctedQuery == ""
}

func (s *Service) slateForPage(
	ctx context.Context,
	query *domain.SearchQuery,
	searchQuery, queryNorm string,
	continues shared.EntityId,
) (rankedResolution, shared.EntityId) {
	if held, ok := s.cache.heldSlate(ctx, continues, queryNorm, query.Kinds); ok {
		return rankedResolution{ranked: held, cached: true}, continues
	}
	return s.resolveRanked(ctx, query, searchQuery, queryNorm), shared.NewEntityId()
}

func (s *Service) resolveRanked(
	ctx context.Context,
	query *domain.SearchQuery,
	searchQuery, queryNorm string,
) rankedResolution {
	if ranked, cached := s.cache.get(ctx, queryNorm, query.Kinds); cached {
		return rankedResolution{ranked: ranked, cached: true}
	}
	if queryNorm == "" {
		return s.resolveCold(ctx, query, searchQuery, queryNorm)
	}

	resolved := s.resolver.resolve(ctx, s.cache.key(queryNorm, query.Kinds), func(flightCtx context.Context) (answer rankedResolution) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(flightCtx, "search.v2.resolve_panic",
					"panic", r, "stack", string(debug.Stack()))
				answer = rankedResolution{partial: true}
			}
		}()
		return s.resolveCold(flightCtx, query, searchQuery, queryNorm)
	})
	resolved.ranked = slices.Clone(resolved.ranked)
	resolved.statuses = slices.Clone(resolved.statuses)
	return resolved
}

func (s *Service) resolveCold(
	ctx context.Context,
	query *domain.SearchQuery,
	searchQuery, queryNorm string,
) rankedResolution {
	var stages stageTimings
	fanOutDone := timeStage(&stages.fanOut)
	perProvider, statuses := s.fanOut(ctx, searchQuery, query.Kinds)
	fanOutDone()
	resolution := rankedResolution{
		ranked:   s.mergeRankEnrich(ctx, perProvider, queryNorm, &stages),
		statuses: statuses,
	}
	if len(resolution.ranked) == 0 {
		resolution = s.correctedResolution(ctx, query, statuses)
	}
	resolution.stages = stages

	resolution.partial = anyProviderFailed(resolution.statuses)
	resolution.pending = anyProviderPending(resolution.statuses)
	if resolution.isAuthoritative() {
		s.cache.set(ctx, queryNorm, query.Kinds, resolution.ranked)
	}
	return resolution
}

func (s *Service) correctedResolution(
	ctx context.Context,
	query *domain.SearchQuery,
	fanOutStatuses []domain.ProviderSearchResponse,
) rankedResolution {
	if AllProvidersFailed(fanOutStatuses) {
		return rankedResolution{statuses: fanOutStatuses}
	}
	correctedQuery, originalQuery, ranked, corrStatuses := s.tryCorrection(ctx, query)
	if correctedQuery == "" {
		return rankedResolution{ranked: ranked, statuses: fanOutStatuses}
	}
	return rankedResolution{
		ranked:         ranked,
		statuses:       mergedStatuses(fanOutStatuses, corrStatuses),
		correctedQuery: correctedQuery,
		originalQuery:  originalQuery,
	}
}

type firstPage struct {
	shown     []domain.SearchResult
	organic   []domain.SearchResult
	fullSlate []domain.SearchResult
	related   []domain.RelatedGroup
	explored  bool
	timing    searchTiming
}

func (s *Service) recordFirstPageSideEffects(
	ctx context.Context,
	run searchRun,
	page firstPage,
) {
	s.history.Record(ctx, run.userId, run.query, run.queryNorm, run.saveHistory, run.startedAt)
	s.telemetry.emit(ctx, run, page, s.ranking.ExplorationRate)
	s.vocab.Ingest(ctx, page.organic)
}

func (s *Service) mergeRankEnrich(
	ctx context.Context,
	perProvider [][]domain.SearchResult,
	queryNorm string,
	stages *stageTimings,
) []domain.SearchResult {
	identityDone := timeStage(&stages.identity)
	s.identity.Stamp(ctx, perProvider)
	identityDone()

	rankDone := timeStage(&stages.rank)
	ranked := rank.RankPipelineWith(perProvider, queryNorm, s.ranking.RankOptions())

	for i := range ranked {
		ranked[i].Signature = domain.ResultSignature(ranked[i])
	}
	rankDone()

	artworkDone := timeStage(&stages.artwork)
	ranked = s.artwork.Fill(ctx, ranked, s.bg)
	artworkDone()
	return ranked
}

func (s *Service) RankVariantsForEval(
	ctx context.Context,
	query *domain.SearchQuery,
) (withReshape, withoutReshape []domain.SearchResult) {
	searchQuery := domain.SearchText(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, _ := s.fanOutLive(ctx, searchQuery, query.Kinds)
	s.identity.Stamp(ctx, perProvider)
	return rank.RankPipeline(perProvider, queryNorm), rank.RankPipelineNoReshape(perProvider, queryNorm)
}

func (s *Service) InspectSearchWithStatuses(
	ctx context.Context,
	query *domain.SearchQuery,
) ([]domain.SearchResult, []domain.ProviderSearchResponse) {
	searchQuery := domain.SearchText(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, statuses := s.fanOutLive(ctx, searchQuery, query.Kinds)
	ranked := s.mergeRankEnrich(ctx, perProvider, queryNorm, &stageTimings{})
	if query.Limit > 0 && len(ranked) > query.Limit {
		ranked = ranked[:query.Limit]
	}
	return ranked, statuses
}

func (s *Service) WaitForBackground() {
	s.bg.Wait()
}

func pageOf(ranked []domain.SearchResult, offset, limit int) []domain.SearchResult {
	if offset >= len(ranked) {
		return nil
	}
	end := offset + limit
	if limit <= 0 || end > len(ranked) {
		end = len(ranked)
	}
	return ranked[offset:end]
}

func (s *Service) RefreshBehavioralScores(ctx context.Context) error {
	return s.ranking.RefreshBehavioralScores(ctx)
}
