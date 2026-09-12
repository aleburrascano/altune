package service

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"

	"github.com/google/uuid"
)

const historyRingSize = 100

type rankingExperiments struct {
	tailDemotion bool

	crossKindProminence bool

	behavioralRanking  bool
	behavioralConsumer ports.EventConsumer
	behavioralScores   atomic.Pointer[map[string]float64]

	explorationRate float64
}

type Service struct {
	providers        []ports.SearchProvider
	circuitBreaker   *CircuitBreaker
	historyRepo      ports.HistoryWriter
	vocabStore       ports.VocabularyStore
	eventStore       ports.EventStore
	artworkResolver  ports.TaggingArtworkResolver
	artworkCache     ports.ArtworkCache
	albumValidator   ports.ArtistIdentityResolver
	identityBridge   ports.IdentityBridge
	mbidIndex        ports.MBIDIndex
	identityStore    ports.IdentityStore
	identityVerifier *IdentityVerifier
	correctionSvc    *CorrectionService
	findRelatedSvc   *FindRelatedService
	resultCache      ports.ResultCache
	favoritesRepo    ports.FavoritesRepository

	rankingExperiments

	bgWg sync.WaitGroup
}

func (s *Service) launchBackground(parentCtx context.Context, label string, fn func(ctx context.Context)) {
	ctx := context.WithoutCancel(parentCtx)
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("search.v2.background_panic", "label", label, "error", r)
			}
		}()
		fn(ctx)
	}()
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
	return func(s *Service) { s.tailDemotion = true }
}

func WithCrossKindProminence() Option {
	return func(s *Service) { s.crossKindProminence = true }
}

func WithBehavioralRanking(consumer ports.EventConsumer) Option {
	return func(s *Service) {
		s.behavioralRanking = true
		s.behavioralConsumer = consumer
	}
}

func WithExploration(rate float64) Option {
	return func(s *Service) {
		if rate > 0 {
			s.explorationRate = rate
		}
	}
}

func (s *Service) maybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	if s.explorationRate <= 0 || len(ranked) < 2 {
		return ranked, false
	}
	if rand.Float64() >= s.explorationRate {
		return ranked, false
	}
	out := slices.Clone(ranked)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, true
}

func NewService(providers []ports.SearchProvider, circuitBreaker *CircuitBreaker, opts ...Option) *Service {
	s := &Service{
		providers:      providers,
		circuitBreaker: circuitBreaker,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.vocabStore != nil {
		s.correctionSvc = NewCorrectionService(s.vocabStore)
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

	slog.InfoContext(ctx, "search.v2.start", "query", query.Raw)

	cacheKey := resultCacheKey(queryNorm, query.Kinds)
	var (
		ranked         []domain.SearchResult
		statuses       []domain.ProviderSearchResponse
		correctedQuery string
		originalQuery  string
		cached         bool
		partial        bool
	)
	useResultCache := s.resultCache != nil && queryNorm != ""
	if useResultCache {
		if hit, ok := s.resultCache.Get(ctx, cacheKey); ok {
			ranked, cached = hit, true
		}
	}

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
		if useResultCache && len(ranked) > 0 && !partial && correctedQuery == "" {
			s.resultCache.Set(ctx, cacheKey, ranked)
		}
	}

	ranked = s.liftFavorites(ctx, userId, ranked)

	var related []domain.RelatedGroup
	if s.findRelatedSvc != nil && len(ranked) > 0 {
		related = s.findRelatedSvc.Execute(ctx, ranked)
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
	}

	if query.Offset == 0 {
		s.persistHistory(ctx, userId, query, queryNorm, saveHistory)
		s.emitSearchEvent(ctx, userId, searchId, queryNorm, ranked, explored)
		ingestQuery := query.Raw
		if correctedQuery != "" {
			ingestQuery = correctedQuery
		}
		s.ingestVocabulary(ctx, ingestQuery, organic)
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

	ranked := rankPipelineWith(perProvider, queryNorm, RankOptions{
		TailDemotion:        s.tailDemotion,
		CrossKindProminence: s.crossKindProminence,
		Behavioral:          s.BehavioralScoresSnapshot(),
	})

	for i := range ranked {
		ranked[i].Signature = domain.ResultSignature(ranked[i])
	}

	ranked = s.applyArtistDisambiguation(ctx, ranked)
	ranked = s.fillArtwork(ctx, ranked)
	return ranked
}

func (s *Service) persistHistory(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	queryNorm string,
	saveHistory bool,
) {
	if !saveHistory || s.historyRepo == nil {
		return
	}
	entry := &domain.SearchHistoryEntry{
		ID:         uuid.New(),
		UserId:     userId,
		Query:      query.Raw,
		QueryNorm:  queryNorm,
		ExecutedAt: time.Now().UTC(),
	}
	if err := s.historyRepo.Insert(ctx, entry); err != nil {
		slog.WarnContext(ctx, "search.v2.history_persist_failed", "error", err)
		return
	}
	if err := s.historyRepo.TrimToN(ctx, userId, historyRingSize); err != nil {
		slog.WarnContext(ctx, "search.v2.history_trim_failed", "error", err)
	}
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
	s.bgWg.Wait()
}

func resultCacheKey(queryNorm string, kinds map[domain.ResultKind]bool) string {
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k.String())
	}
	sort.Strings(ks)
	return queryNorm + "|" + strings.Join(ks, ",")
}
