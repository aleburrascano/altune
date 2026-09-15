package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// Service is the discovery search orchestrator. It owns only the fan-out, the
// rank/merge sequencing, and the order in which the per-responsibility
// collaborators below run; each responsibility (identity stamping, artist
// disambiguation, artwork fill, ranking experiments, favorites lift, result
// caching, correction retry, related aggregation, history persistence,
// telemetry, vocabulary ingest) lives in its own unit and can change without
// touching the others.
type Service struct {
	providers      []ports.SearchProvider
	circuitBreaker *CircuitBreaker

	// Per-responsibility collaborators, built by NewService from the
	// dependencies the With* options capture.
	identity       *IdentityStamper
	disambiguator  *artistDisambiguator
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
}

// SearchOutput is the result of one search. QueryNorm is the canonical
// normalized query (NormalizeForMatch of the cleaned query) the service used
// for its cache key, history, and telemetry; callers must report it rather
// than re-normalizing the raw query.
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
	Slate            BlendedSlate
}

// serviceConfig collects the dependencies the With* options capture. It exists
// only during NewService, which threads each dependency into the collaborator
// that owns it, so the orchestrator itself never holds a raw port.
type serviceConfig struct {
	historyRepo   ports.HistoryWriter
	vocabStore    ports.VocabularyStore
	eventStore    ports.EventStore
	resultCache   ports.ResultCache
	favoritesRepo ports.FavoritesRepository

	artworkResolver  ports.TaggingArtworkResolver
	artworkCache     ports.ArtworkCache
	albumValidator   ports.ArtistIdentityResolver
	identityBridge   ports.IdentityBridge
	mbidIndex        ports.MBIDIndex
	identityStore    ports.IdentityStore
	identityVerifier *IdentityVerifier

	findRelatedSvc *FindRelatedService

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

func WithArtworkResolver(r ports.TaggingArtworkResolver) Option {
	return func(c *serviceConfig) { c.artworkResolver = r }
}

func WithArtworkCache(ac ports.ArtworkCache) Option {
	return func(c *serviceConfig) { c.artworkCache = ac }
}

func WithAlbumValidator(v ports.ArtistIdentityResolver) Option {
	return func(c *serviceConfig) { c.albumValidator = v }
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

// maybeExplore delegates to the ranking-experiments collaborator.
func (s *Service) maybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	return s.ranking.maybeExplore(ranked)
}

func NewService(providers []ports.SearchProvider, circuitBreaker *CircuitBreaker, opts ...Option) *Service {
	var cfg serviceConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	bg := &backgroundRunner{}
	s := &Service{
		providers:      providers,
		circuitBreaker: circuitBreaker,
		identity:       newIdentityStamper(cfg.identityBridge, cfg.identityStore, cfg.identityVerifier, bg),
		disambiguator:  newArtistDisambiguator(cfg.albumValidator),
		artwork:        newArtworkFiller(cfg.artworkResolver, cfg.artworkCache, cfg.identityStore, cfg.mbidIndex),
		ranking:        newRankingExperiments(cfg.ranking, bg),
		favorites:      newFavoritesLifter(cfg.favoritesRepo),
		findRelatedSvc: cfg.findRelatedSvc,
		cache:          newSearchResultCache(cfg.resultCache),
		history:        NewRecordSearchHistoryService(cfg.historyRepo),
		telemetry:      newSearchTelemetry(cfg.eventStore, bg),
		vocab:          newVocabularyIngestor(cfg.vocabStore, bg),
		bg:             bg,
	}
	if cfg.vocabStore != nil {
		s.correctionSvc = NewCorrectionService(cfg.vocabStore)
	}
	return s
}

func (s *Service) Execute(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	saveHistory bool,
) (*SearchOutput, error) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)

	searchId := uuid.New().String()

	slog.InfoContext(ctx, "search.v2.start", logging.SearchTextAttr(query.Raw))

	var (
		statuses       []domain.ProviderSearchResponse
		correctedQuery string
		originalQuery  string
		partial        bool
	)
	ranked, cached := s.cache.get(ctx, queryNorm, query.Kinds)

	if !cached {
		var perProvider [][]domain.SearchResult
		perProvider, statuses = s.fanOut(ctx, searchQuery, query.Kinds)
		ranked = s.mergeRankEnrich(ctx, perProvider, queryNorm)

		if len(ranked) == 0 {
			var corrStatuses []domain.ProviderSearchResponse
			correctedQuery, originalQuery, ranked, corrStatuses = s.tryCorrection(ctx, query)
			if correctedQuery != "" {
				statuses = corrStatuses
			}
		}

		partial = anyProviderFailed(statuses)
		if len(ranked) > 0 && !partial && correctedQuery == "" {
			s.cache.set(ctx, queryNorm, query.Kinds, ranked)
		}
	}

	ranked = s.favorites.lift(ctx, userId, ranked)

	var related []domain.RelatedGroup
	if s.findRelatedSvc != nil && len(ranked) > 0 {
		related = s.findRelatedSvc.Execute(ctx, userId, ranked)
	}

	total := len(ranked)
	fullSlate := ranked
	ranked = pageOf(ranked, query.Offset, query.Limit)
	hasMore := query.Offset+len(ranked) < total

	organic := ranked
	explored := false
	var slate BlendedSlate
	if query.Offset == 0 {
		ranked, explored = s.maybeExplore(ranked)
		slate = BuildBlendedSlate(ranked, fullSlate)

		s.history.Record(ctx, userId, query, queryNorm, saveHistory)
		s.telemetry.emit(ctx, userId, searchId, queryNorm, ranked,
			shownSignatures(fullSlate, related), explored, s.ranking.explorationRate)
		ingestQuery := query.Raw
		if correctedQuery != "" {
			ingestQuery = correctedQuery
		}
		s.vocab.ingest(ctx, ingestQuery, organic)
	}

	slog.InfoContext(ctx, "search.v2.complete",
		logging.SearchTextAttr(query.Raw),
		"results", len(ranked),
		"partial", partial,
		"corrected", correctedQuery != "",
		"related_groups", len(related),
		"cached", cached,
		"offset", query.Offset,
		"total", total,
		"tail_noise_top5", TailNoiseInTopK(ranked, 5),
	)

	return &SearchOutput{
		SearchId:         searchId,
		QueryNorm:        queryNorm,
		Explored:         explored,
		Results:          ranked,
		Total:            total,
		Offset:           query.Offset,
		HasMore:          hasMore,
		Slate:            slate,
		ProviderStatuses: statuses,
		Partial:          partial,
		CorrectedQuery:   correctedQuery,
		OriginalQuery:    originalQuery,
		Related:          related,
	}, nil
}

func (s *Service) mergeRankEnrich(
	ctx context.Context,
	perProvider [][]domain.SearchResult,
	queryNorm string,
) []domain.SearchResult {
	s.identity.stamp(ctx, perProvider)

	ranked := rankPipelineWith(perProvider, queryNorm, s.ranking.rankOptions())

	for i := range ranked {
		ranked[i].Signature = domain.ResultSignature(ranked[i])
	}

	ranked = s.disambiguator.apply(ctx, ranked)
	ranked = s.artwork.fill(ctx, ranked)
	return ranked
}

func (s *Service) RankVariantsForEval(
	ctx context.Context,
	query *domain.SearchQuery,
) (withReshape, withoutReshape []domain.SearchResult) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, _ := s.fanOut(ctx, searchQuery, query.Kinds)
	s.identity.stamp(ctx, perProvider)
	return rankPipeline(perProvider, queryNorm), rankPipelineNoReshape(perProvider, queryNorm)
}

func (s *Service) InspectSearch(ctx context.Context, query *domain.SearchQuery) []domain.SearchResult {
	results, _ := s.InspectSearchWithStatuses(ctx, query)
	return results
}

// InspectSearchWithStatuses runs the inspection fan-out and returns the ranked
// results alongside each provider's status. The statuses let callers tell a
// genuine zero-result query apart from a total upstream outage, which both
// collapse to an empty result set otherwise.
func (s *Service) InspectSearchWithStatuses(
	ctx context.Context,
	query *domain.SearchQuery,
) ([]domain.SearchResult, []domain.ProviderSearchResponse) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, statuses := s.fanOut(ctx, searchQuery, query.Kinds)
	ranked := s.mergeRankEnrich(ctx, perProvider, queryNorm)
	if query.Limit > 0 && len(ranked) > query.Limit {
		ranked = ranked[:query.Limit]
	}
	return ranked, statuses
}

func (s *Service) WaitForBackground() {
	s.bg.wait()
}
