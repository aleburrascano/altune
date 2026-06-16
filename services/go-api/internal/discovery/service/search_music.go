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
	enrichLimit = 25
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
	return s
}

type SearchOutput struct {
	Results          []domain.SearchResult
	ProviderStatuses []domain.ProviderSearchResponse
	Partial          bool
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
			results, err := p.Search(provCtx, query.Raw, query.Kinds)
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

	merged := FuseAndRank(perProvider, queryNorm, nil)

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
		go s.ingestToVocabulary(query.Raw, merged)
	}

	slog.InfoContext(ctx, "search.complete",
		"results", len(merged),
		"partial", partial,
		"duration", time.Since(searchStart),
	)

	return &SearchOutput{
		Results:          merged,
		ProviderStatuses: statuses,
		Partial:          partial,
	}, nil
}

const vocabIngestTop = 3

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
			extras["popularity"] = pop
			changed = true
			slog.DebugContext(ctx, "enrich.popularity",
				"title", result.Title,
				"artist", result.Subtitle,
				"pop", pop,
			)
		} else if extras["popularity"] != nil {
			extras["popularity"] = 0.0
			changed = true
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
