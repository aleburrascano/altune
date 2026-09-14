package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// Service is the discovery search orchestrator. Execute only sequences the
// per-responsibility collaborators below; each responsibility (result caching,
// fan-out/merge/rank, correction retry, favorites lift, related aggregation,
// pagination, ranking experiments, history persistence, telemetry, vocabulary
// ingest) lives in its own unit and can change without touching the others.
type Service struct {
	providers      []ports.SearchProvider
	circuitBreaker *CircuitBreaker

	// Injected dependencies captured by the With* options. NewService threads
	// these into the collaborators below; a few are also read directly.
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

	// Per-responsibility collaborators.
	correctionSvc  *CorrectionService
	findRelatedSvc *FindRelatedService
	ranking        rankingExperiments
	cache          *searchResultCache
	history        *RecordSearchHistoryService
	telemetry      *SearchTelemetry
	vocab          *VocabularyIngestor

	bg *backgroundRunner
}

type SearchOutput struct {
	SearchId         string
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

type Option func(*Service)

func WithHistoryRepository(r ports.HistoryWriter) Option {
	return func(s *Service) { s.historyRepo = r }
}

func WithVocabularyStore(v ports.VocabularyStore) Option {
	return func(s *Service) { s.vocabStore = v }
}

func WithEventStore(e ports.EventStore) Option {
	return func(s *Service) { s.eventStore = e }
}

func WithArtworkResolver(r ports.TaggingArtworkResolver) Option {
	return func(s *Service) { s.artworkResolver = r }
}

func WithArtworkCache(c ports.ArtworkCache) Option {
	return func(s *Service) { s.artworkCache = c }
}

func WithAlbumValidator(v ports.ArtistIdentityResolver) Option {
	return func(s *Service) { s.albumValidator = v }
}

func WithIdentityBridge(b ports.IdentityBridge) Option {
	return func(s *Service) { s.identityBridge = b }
}

func WithMBIDIndex(idx ports.MBIDIndex) Option {
	return func(s *Service) { s.mbidIndex = idx }
}

func WithIdentityStore(store ports.IdentityStore) Option {
	return func(s *Service) { s.identityStore = store }
}

func WithIdentityVerifier(v *IdentityVerifier) Option {
	return func(s *Service) { s.identityVerifier = v }
}

func WithFindRelatedService(r *FindRelatedService) Option {
	return func(s *Service) { s.findRelatedSvc = r }
}

func WithResultCache(c ports.ResultCache) Option {
	return func(s *Service) { s.resultCache = c }
}

func WithFavorites(repo ports.FavoritesRepository) Option {
	return func(s *Service) { s.favoritesRepo = repo }
}

func WithTailDemotion() Option {
	return func(s *Service) { s.ranking.tailDemotion = true }
}

func WithCrossKindProminence() Option {
	return func(s *Service) { s.ranking.crossKindProminence = true }
}

func WithBehavioralRanking(consumer *SatisfactionConsumer) Option {
	return func(s *Service) {
		s.ranking.behavioralRanking = true
		s.ranking.behavioralConsumer = consumer
	}
}

func WithExploration(rate float64) Option {
	return func(s *Service) {
		if rate > 0 {
			s.ranking.explorationRate = rate
		}
	}
}

// maybeExplore delegates to the ranking-experiments collaborator.
func (s *Service) maybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	return s.ranking.maybeExplore(ranked)
}

func NewService(providers []ports.SearchProvider, circuitBreaker *CircuitBreaker, opts ...Option) *Service {
	s := &Service{
		providers:      providers,
		circuitBreaker: circuitBreaker,
		bg:             &backgroundRunner{},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.ranking.bg = s.bg
	if s.vocabStore != nil {
		s.correctionSvc = NewCorrectionService(s.vocabStore)
	}
	s.cache = newSearchResultCache(s.resultCache)
	s.history = NewRecordSearchHistoryService(s.historyRepo)
	s.telemetry = newSearchTelemetry(s.eventStore, s.bg)
	s.vocab = newVocabularyIngestor(s.vocabStore, s.bg)
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

	slog.InfoContext(ctx, "search.v2.start", "query", query.Raw)

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

	ranked = s.liftFavorites(ctx, userId, ranked)

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
		"query", query.Raw,
		"results", len(ranked),
		"partial", partial,
		"corrected", correctedQuery,
		"related_groups", len(related),
		"cached", cached,
		"offset", query.Offset,
		"total", total,
		"tail_noise_top5", TailNoiseInTopK(ranked, 5),
	)

	return &SearchOutput{
		SearchId:         searchId,
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
	s.stampIdentities(ctx, perProvider)

	ranked := rankPipelineWith(perProvider, queryNorm, s.ranking.rankOptions())

	for i := range ranked {
		ranked[i].Signature = domain.ResultSignature(ranked[i])
	}

	ranked = s.applyArtistDisambiguation(ctx, ranked)
	ranked = s.fillArtwork(ctx, ranked)
	return ranked
}

func (s *Service) RankVariantsForEval(
	ctx context.Context,
	query *domain.SearchQuery,
) (withReshape, withoutReshape []domain.SearchResult) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, _ := s.fanOut(ctx, searchQuery, query.Kinds)
	s.stampIdentities(ctx, perProvider)
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
