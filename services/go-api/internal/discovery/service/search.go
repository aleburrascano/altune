package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"runtime/debug"
	"slices"
	"time"
)

type Service struct {
	providers      []ports.SearchProvider
	circuitBreaker *CircuitBreaker
	providerCache  ports.ProviderResultCache
	deadline       time.Duration

	identity       *IdentityStamper
	artwork        *ArtworkFiller
	ranking        *RankingExperiments
	favorites      *favoritesLifter
	correctionSvc  *CorrectionService
	findRelatedSvc *FindRelatedService
	cache          *searchResultCache
	history        *RecordSearchHistoryService
	telemetry      *SearchTelemetry
	vocab          *VocabularyIngestor

	bg *backgroundRunner

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
	identityVerifier *IdentityVerifier

	findRelatedSvc *FindRelatedService

	searchDeadline time.Duration

	ranking rankingConfig
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

func WithIdentityVerifier(v *IdentityVerifier) Option {
	return func(c *serviceConfig) { c.identityVerifier = v }
}

func WithFindRelatedService(r *FindRelatedService) Option {
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
	return func(c *serviceConfig) { c.ranking.tailDemotion = true }
}

func WithCrossKindProminence() Option {
	return func(c *serviceConfig) { c.ranking.crossKindProminence = true }
}

func WithBehavioralRanking(consumer *SatisfactionConsumer) Option {
	return func(c *serviceConfig) {
		c.ranking.behavioralRanking = true
		c.ranking.behavioralConsumer = consumer
	}
}

func WithExploration(rate float64) Option {
	return func(c *serviceConfig) {
		if rate > 0 {
			c.ranking.explorationRate = rate
		}
	}
}

func (s *Service) maybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	return s.ranking.maybeExplore(ranked)
}

func (s *Service) CircuitBreaker() *CircuitBreaker {
	return s.circuitBreaker
}

func NewService(providers []ports.SearchProvider, circuitBreaker *CircuitBreaker, opts ...Option) *Service {
	var cfg serviceConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	bg := &backgroundRunner{}
	heldSlateCache := cfg.heldSlateCache
	if heldSlateCache == nil {
		heldSlateCache = cfg.resultCache
	}
	s := &Service{
		providers:      providers,
		circuitBreaker: circuitBreaker,
		providerCache:  cfg.providerCache,
		deadline:       cfg.searchDeadline,
		identity:       newIdentityStamper(cfg.identityBridge, cfg.identityStore, cfg.identityVerifier, bg),
		artwork:        newArtworkFiller(cfg.artworkResolver, cfg.artworkCache, cfg.identityStore, cfg.mbidIndex),
		ranking:        newRankingExperiments(cfg.ranking, bg),
		favorites:      newFavoritesLifter(cfg.favoritesRepo),
		findRelatedSvc: cfg.findRelatedSvc,
		cache:          newSearchResultCache(cfg.resultCache, heldSlateCache),
		history:        NewRecordSearchHistoryService(cfg.historyRepo),
		telemetry:      newSearchTelemetry(cfg.eventStore, cfg.activityFeed, bg),
		vocab:          newVocabularyIngestor(cfg.vocabStore, bg),
		bg:             bg,
		resolver:       newSharedResolver(),
	}
	if cfg.vocabStore != nil {
		s.correctionSvc = NewCorrectionService(cfg.vocabStore)
	}
	return s
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
	searchQuery := CleanQuery(query.Raw)
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
	ranked := s.favorites.lift(ctx, userId, resolution.ranked)
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
		shown, explored = s.maybeExplore(organic)
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
		"tail_noise_top5", TailNoiseInTopK(shown, 5),
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
	s.telemetry.emit(ctx, run, page, s.ranking.explorationRate)
	s.vocab.ingest(ctx, page.organic)
}

func (s *Service) mergeRankEnrich(
	ctx context.Context,
	perProvider [][]domain.SearchResult,
	queryNorm string,
	stages *stageTimings,
) []domain.SearchResult {
	identityDone := timeStage(&stages.identity)
	s.identity.stamp(ctx, perProvider)
	identityDone()

	rankDone := timeStage(&stages.rank)
	ranked := rankPipelineWith(perProvider, queryNorm, s.ranking.rankOptions())

	for i := range ranked {
		ranked[i].Signature = domain.ResultSignature(ranked[i])
	}
	rankDone()

	artworkDone := timeStage(&stages.artwork)
	ranked = s.artwork.fill(ctx, ranked, s.bg)
	artworkDone()
	return ranked
}

func (s *Service) RankVariantsForEval(
	ctx context.Context,
	query *domain.SearchQuery,
) (withReshape, withoutReshape []domain.SearchResult) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, _ := s.fanOutLive(ctx, searchQuery, query.Kinds)
	s.identity.stamp(ctx, perProvider)
	return rankPipeline(perProvider, queryNorm), rankPipelineNoReshape(perProvider, queryNorm)
}

func (s *Service) InspectSearchWithStatuses(
	ctx context.Context,
	query *domain.SearchQuery,
) ([]domain.SearchResult, []domain.ProviderSearchResponse) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, statuses := s.fanOutLive(ctx, searchQuery, query.Kinds)
	ranked := s.mergeRankEnrich(ctx, perProvider, queryNorm, &stageTimings{})
	if query.Limit > 0 && len(ranked) > query.Limit {
		ranked = ranked[:query.Limit]
	}
	return ranked, statuses
}

func (s *Service) WaitForBackground() {
	s.bg.wait()
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
