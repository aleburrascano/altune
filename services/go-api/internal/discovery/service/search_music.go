package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

const (
	// enrichLimit caps artwork/popularity enrichment to the top N results to bound latency.
	enrichLimit = 50
	// enrichConcurrency limits parallel enrichment goroutines to avoid overwhelming providers.
	enrichConcurrency = 8
)

type SearchMusicService struct {
	providers          []ports.SearchProvider
	queryCache         ports.QueryCache
	historyRepo        ports.SearchHistoryRepository
	circuitBreaker     *CircuitBreaker
	popularityResolver ports.PopularityResolver
	artworkResolver    ports.ArtworkResolver
	artworkCache       ports.ArtworkCache
	fanartResolver     ports.ArtworkResolver
	geniusResolver     ports.ArtworkResolver
	vocabStore         ports.VocabularyStore
	correctionSvc      *CorrectionService
	findRelatedSvc     *FindRelatedService
}

type SearchOption func(*SearchMusicService)

func WithPopularityResolver(r ports.PopularityResolver) SearchOption {
	return func(s *SearchMusicService) { s.popularityResolver = r }
}

func WithArtworkResolver(r ports.ArtworkResolver) SearchOption {
	return func(s *SearchMusicService) { s.artworkResolver = r }
}

func WithArtworkCache(c ports.ArtworkCache) SearchOption {
	return func(s *SearchMusicService) { s.artworkCache = c }
}

func WithFanartResolver(r ports.ArtworkResolver) SearchOption {
	return func(s *SearchMusicService) { s.fanartResolver = r }
}

func WithGeniusResolver(r ports.ArtworkResolver) SearchOption {
	return func(s *SearchMusicService) { s.geniusResolver = r }
}

func WithVocabularyStore(v ports.VocabularyStore) SearchOption {
	return func(s *SearchMusicService) { s.vocabStore = v }
}

func NewSearchMusicService(
	providers []ports.SearchProvider,
	queryCache ports.QueryCache,
	historyRepo ports.SearchHistoryRepository,
	circuitBreaker *CircuitBreaker,
	opts ...SearchOption,
) *SearchMusicService {
	s := &SearchMusicService{
		providers:      providers,
		queryCache:     queryCache,
		historyRepo:    historyRepo,
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

type SearchOutput struct {
	Results          []domain.SearchResult
	ProviderStatuses []domain.ProviderSearchResponse
	Partial          bool
	CorrectedQuery   string
	OriginalQuery    string
	SuggestedQuery   string
	Related          []domain.RelatedGroup
}

func kindsString(kinds map[domain.ResultKind]bool) string {
	var parts []string
	for k := range kinds {
		parts = append(parts, k.String())
	}
	return strings.Join(parts, ",")
}

func (s *SearchMusicService) Execute(ctx context.Context, userId shared.UserId, query *domain.SearchQuery, saveHistory bool) (*SearchOutput, error) {
	queryNorm := NormalizeForMatch(query.Raw)
	if query.QueryNorm == "" {
		query.QueryNorm = queryNorm
	}

	slog.InfoContext(ctx, "search.start",
		"query", query.Raw,
		"kinds", kindsString(query.Kinds),
		"limit", query.Limit,
		"user_id", userId.String(),
	)

	searchStart := time.Now()

	searchQuery := CleanQuery(query.Raw)
	if searchQuery != query.Raw {
		queryNorm = NormalizeForMatch(searchQuery)
		slog.DebugContext(ctx, "search.query_cleaned",
			"original", query.Raw,
			"cleaned", searchQuery,
		)
	}

	intent := DetectIntent(ctx, queryNorm, s.vocabStore)

	var (
		mu          sync.Mutex
		perProvider [][]domain.SearchResult
		statuses    []domain.ProviderSearchResponse
		wg          sync.WaitGroup
	)

	for _, provider := range s.providers {
		if !s.circuitBreaker.AllowRequest(provider.Name()) {
			slog.WarnContext(ctx, "provider.circuit_open",
				"provider", provider.Name().String())
			mu.Lock()
			statuses = append(statuses, domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			})
			mu.Unlock()
			continue
		}

		slog.InfoContext(ctx, "provider.search",
			"provider", provider.Name().String(),
			"kinds", kindsString(query.Kinds),
		)

		wg.Add(1)
		go func(p ports.SearchProvider) {
			defer wg.Done()

			timeout := 1500 * time.Millisecond
			if tp, ok := p.(interface{ SearchTimeout() time.Duration }); ok {
				timeout = tp.SearchTimeout()
			}
			provCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			start := time.Now()
			var results []domain.SearchResult
			var err error
			if intent != nil {
				if ss, ok := p.(ports.StructuredSearcher); ok {
					results, err = ss.SearchStructured(provCtx, intent.Artist, intent.Track, query.Kinds)
				}
			}
			if results == nil && err == nil {
				results, err = p.Search(provCtx, searchQuery, query.Kinds)
			}
			latencyMs := time.Since(start).Milliseconds()

			if err != nil {
				s.circuitBreaker.RecordFailure(p.Name())
				status := domain.ProviderStatusError
				if provCtx.Err() != nil {
					status = domain.ProviderStatusTimeout
				}
				mu.Lock()
				statuses = append(statuses, domain.ProviderSearchResponse{
					Provider:  p.Name(),
					Status:    status,
					LatencyMs: latencyMs,
				})
				mu.Unlock()
				slog.WarnContext(ctx, "provider.failed",
					"provider", p.Name().String(),
					"status", status.String(),
					"latency_ms", latencyMs,
					"error", err,
				)
				return
			}

			s.circuitBreaker.RecordSuccess(p.Name())
			mu.Lock()
			perProvider = append(perProvider, results)
			statuses = append(statuses, domain.ProviderSearchResponse{
				Provider:    p.Name(),
				Results:     results,
				Status:      domain.ProviderStatusOK,
				LatencyMs:   latencyMs,
				ResultCount: len(results),
			})
			mu.Unlock()

			slog.InfoContext(ctx, "provider.complete",
				"provider", p.Name().String(),
				"status", "ok",
				"results", len(results),
				"latency_ms", latencyMs,
			)
		}(provider)
	}

	wg.Wait()

	rawCount := 0
	for _, group := range perProvider {
		rawCount += len(group)
	}

	scorer := func(r domain.SearchResult) domain.QualityScore {
		return ComputeQualityScore(r, 1.0)
	}
	merged := FuseAndRank(perProvider, queryNorm, scorer, intent)

	enriching := enrichLimit
	if len(merged) < enriching {
		enriching = len(merged)
	}

	slog.InfoContext(ctx, "search.merged",
		"raw", rawCount,
		"merged", len(merged),
		"enriching", enriching,
	)

	merged = s.enrich(ctx, merged)
	merged = Rerank(merged, queryNorm)

	var correctedQuery, originalQuery, suggestedQuery string
	if len(merged) == 0 {
		correctedQuery, originalQuery = s.tryCorrection(ctx, query, queryNorm, &merged, &statuses)
	}

	if len(merged) > 0 && correctedQuery == "" {
		suggestedQuery = s.suggestIfLowRelevance(ctx, merged, query.Raw, queryNorm)
	}

	var related []domain.RelatedGroup
	if s.findRelatedSvc != nil && len(merged) > 0 {
		related = s.findRelatedSvc.Execute(ctx, merged)
	}

	if len(merged) > query.Limit {
		merged = merged[:query.Limit]
	}

	partial := false
	for _, st := range statuses {
		if st.Status != domain.ProviderStatusOK {
			partial = true
			break
		}
	}

	if saveHistory && s.historyRepo != nil {
		entry := &domain.SearchHistoryEntry{
			ID:         uuid.New(),
			UserId:     userId,
			Query:      query.Raw,
			QueryNorm:  queryNorm,
			ExecutedAt: time.Now().UTC(),
		}
		if err := s.historyRepo.Insert(ctx, entry); err != nil {
			slog.WarnContext(ctx, "search.history_persist_failed", "error", err)
		}
	}

	if len(merged) > 0 && s.vocabStore != nil {
		ingestQuery := query.Raw
		if correctedQuery != "" {
			ingestQuery = correctedQuery
		}
		go s.ingestToVocabulary(ingestQuery, merged)
	}

	slog.InfoContext(ctx, "search.complete",
		"results", len(merged),
		"partial", partial,
		"corrected", correctedQuery,
		"duration", time.Since(searchStart),
	)

	return &SearchOutput{
		Results:          merged,
		ProviderStatuses: statuses,
		Partial:          partial,
		CorrectedQuery:   correctedQuery,
		OriginalQuery:    originalQuery,
		SuggestedQuery:   suggestedQuery,
		Related:          related,
	}, nil
}

func (s *SearchMusicService) preQueryCorrection(
	ctx context.Context,
	rawQuery string,
	queryNorm string,
) *CorrectionResult {
	if s.correctionSvc == nil {
		return nil
	}
	result := s.correctionSvc.Correct(ctx, rawQuery)
	if result == nil {
		return nil
	}
	corrNorm := NormalizeForMatch(result.Corrected)
	if corrNorm == queryNorm {
		return nil
	}
	slog.InfoContext(ctx, "search.pre_correction",
		"original", rawQuery,
		"corrected", result.Corrected,
		"confidence", result.Confidence,
	)
	return result
}

func (s *SearchMusicService) tryCorrection(
	ctx context.Context,
	query *domain.SearchQuery,
	queryNorm string,
	merged *[]domain.SearchResult,
	statuses *[]domain.ProviderSearchResponse,
) (correctedQuery, originalQuery string) {
	if s.correctionSvc == nil {
		return "", ""
	}
	result := s.correctionSvc.CorrectAggressive(ctx, query.Raw)
	if result == nil {
		return "", ""
	}
	corrNorm := NormalizeForMatch(result.Corrected)
	if corrNorm == queryNorm {
		return "", ""
	}

	slog.InfoContext(ctx, "search.correcting",
		"original", query.Raw,
		"corrected", result.Corrected,
		"confidence", result.Confidence,
	)

	retried := s.retrySearch(ctx, result.Corrected, query.Kinds)
	retryScorer := func(r domain.SearchResult) domain.QualityScore {
		return ComputeQualityScore(r, 1.0)
	}
	*merged = FuseAndRank(retried, corrNorm, retryScorer, nil)
	*merged = s.enrich(ctx, *merged)
	*merged = Rerank(*merged, corrNorm)

	return result.Corrected, query.Raw
}

func (s *SearchMusicService) retrySearch(
	ctx context.Context,
	correctedQuery string,
	kinds map[domain.ResultKind]bool,
) [][]domain.SearchResult {
	var (
		mu          sync.Mutex
		perProvider [][]domain.SearchResult
		wg          sync.WaitGroup
	)
	for _, p := range s.providers {
		if !s.circuitBreaker.AllowRequest(p.Name()) {
			continue
		}
		wg.Add(1)
		go func(p ports.SearchProvider) {
			defer wg.Done()
			provCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			results, err := p.Search(provCtx, correctedQuery, kinds)
			if err != nil {
				return
			}
			mu.Lock()
			perProvider = append(perProvider, results)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return perProvider
}

const lowRelevanceThreshold = 0.3

func (s *SearchMusicService) suggestIfLowRelevance(
	ctx context.Context,
	results []domain.SearchResult,
	rawQuery string,
	queryNorm string,
) string {
	if s.correctionSvc == nil || len(results) == 0 {
		return ""
	}
	topRelevance := relevanceScore(results[0], queryNorm)
	if topRelevance >= lowRelevanceThreshold {
		return ""
	}
	result := s.correctionSvc.Correct(ctx, rawQuery)
	if result == nil {
		return ""
	}
	corrNorm := NormalizeForMatch(result.Corrected)
	if corrNorm == queryNorm {
		return ""
	}
	slog.InfoContext(ctx, "search.suggestion",
		"original", rawQuery,
		"suggested", result.Corrected,
		"top_relevance", topRelevance,
		"confidence", result.Confidence,
	)
	return result.Corrected
}

const vocabIngestTop = 5

func (s *SearchMusicService) ingestToVocabulary(rawQuery string, results []domain.SearchResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("search.vocab_ingest_panic", "error", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	entries := buildVocabEntries(rawQuery, results)
	for _, e := range entries {
		_ = s.vocabStore.Add(ctx, e)
	}
}

func buildVocabEntries(rawQuery string, results []domain.SearchResult) []domain.VocabularyEntry {
	entries := []domain.VocabularyEntry{{
		Term:     rawQuery,
		TermNorm: NormalizeForMatch(rawQuery),
		Kind:     "query",
	}}
	limit := vocabIngestTop
	if len(results) < limit {
		limit = len(results)
	}
	for _, r := range results[:limit] {
		text := r.Title
		if r.Subtitle != "" {
			text = r.Title + " - " + r.Subtitle
		}
		entries = append(entries, domain.VocabularyEntry{
			Term:       text,
			TermNorm:   NormalizeForMatch(text),
			Kind:       r.Kind.String(),
			Popularity: int64(popularity(r)),
		})
		if r.Subtitle != "" && r.Kind == domain.ResultKindTrack {
			entries = append(entries, domain.VocabularyEntry{
				Term:       r.Subtitle,
				TermNorm:   NormalizeForMatch(r.Subtitle),
				Kind:       "artist",
				Popularity: int64(popularity(r)),
			})
		}
	}
	return entries
}

const enrichTimeout = 4 * time.Second

func (s *SearchMusicService) enrich(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if s.popularityResolver == nil && s.artworkResolver == nil && s.fanartResolver == nil && s.geniusResolver == nil {
		return results
	}

	limit := enrichLimit
	if len(results) < limit {
		limit = len(results)
	}
	if limit == 0 {
		return results
	}

	enrichCtx, cancel := context.WithTimeout(ctx, enrichTimeout)
	defer cancel()

	top := results[:limit]
	rest := results[limit:]

	sem := make(chan struct{}, enrichConcurrency)
	var wg sync.WaitGroup
	enriched := make([]domain.SearchResult, len(top))

	for i, r := range top {
		wg.Add(1)
		go func(idx int, result domain.SearchResult) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			enriched[idx] = s.enrichOne(enrichCtx, result)
		}(i, r)
	}

	wg.Wait()
	return append(enriched, rest...)
}

const emptyArtHash = "d41d8cd98f00b204e9800998ecf8427e"

func (s *SearchMusicService) enrichOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	extras := copyExtras(result.Extras)
	imageURL := result.ImageURL
	changed := false

	if s.popularityResolver != nil {
		pop, err := s.popularityResolver.GetPopularity(ctx, result.Title, result.Subtitle)
		if err == nil && pop > 0 {
			existing := parseIntLike(extras["popularity"])
			best := maxI64(pop, existing)
			extras["popularity"] = best
			changed = true
			slog.DebugContext(ctx, "enrich.popularity",
				"title", result.Title,
				"artist", result.Subtitle,
				"resolved", pop,
				"existing", existing,
				"used", best,
			)
		}
	}

	needsArt := imageURL == "" || strings.Contains(imageURL, emptyArtHash)
	tryArt := needsArt || result.Kind == domain.ResultKindArtist
	mbid := getStringExtra(result, "mbid")

	if tryArt && s.artworkCache != nil {
		cachedURL, found, _ := s.artworkCache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if found {
			if cachedURL != "" {
				imageURL = cachedURL
				changed = true
				slog.DebugContext(ctx, "enrich.artwork",
					"title", result.Title, "source", "cache_hit")
			}
			tryArt = false
		}
	}

	resolvedArt := s.resolveArtwork(ctx, result, mbid, needsArt)

	if resolvedArt != "" {
		imageURL = resolvedArt
		changed = true
	}

	if tryArt && s.artworkCache != nil {
		_ = s.artworkCache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolvedArt)
	}

	if !changed {
		if needsArt {
			slog.DebugContext(ctx, "enrich.artwork_miss",
				"kind", result.Kind.String(),
				"title", result.Title,
				"subtitle", result.Subtitle,
				"has_mbid", mbid != "")
		}
		return result
	}

	result.ImageURL = imageURL
	result.Extras = extras
	return result
}

func (s *SearchMusicService) resolveArtwork(ctx context.Context, result domain.SearchResult, mbid string, needsArt bool) string {
	if s.fanartResolver != nil && mbid != "" {
		url, _ := s.fanartResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if url != "" {
			slog.DebugContext(ctx, "enrich.artwork",
				"title", result.Title, "source", "fanart")
			return url
		}
	}

	if needsArt && s.geniusResolver != nil {
		url, _ := s.geniusResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if url != "" {
			slog.DebugContext(ctx, "enrich.artwork",
				"title", result.Title, "source", "genius")
			return url
		}
	}

	if needsArt && s.artworkResolver != nil {
		url, _ := s.artworkResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if url != "" {
			slog.DebugContext(ctx, "enrich.artwork",
				"title", result.Title, "source", "chain")
			return url
		}
	}

	return ""
}
