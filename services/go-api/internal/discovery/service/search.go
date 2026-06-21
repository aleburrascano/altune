// Package service is the discovery search pipeline plus the surrounding
// use cases (suggest, consensus, artist/album content, clicks, history,
// vocabulary learning).
//
// The search path (Service.Execute) is the rebuilt Merge → Rank core. Its
// design doctrine: zero arbitrary, query-fit constants. Continuous/multi-signal
// judgments are structural decisions (identifier-first merge, continuous
// relevance) instead of tuned thresholds. Surviving numbers must be principled
// (SLA timeouts, RRF k=60), learned-later (the Layer-3 ML seam, plan 004), or a
// single documented last resort the top-K eval proves generalizes. The
// behavioral click-boost is intentionally dropped: it is a learned signal for
// the ML seam, not a hand-tuned constant.
//
// AIDEV-NOTE: This package is the result of collapsing the strangler rebuild
// (formerly internal/discovery2) back into the discovery context — the rebuilt
// Merge/Rank replaced the v1 ranking chain (FuseAndRank/Rerank/CollapseVersions/
// ApplyPopularityDominance + quality/intent/popularity machinery), which is
// deleted. Result-shaping rules it still relies on (EnforceDiversity,
// CollapseArtistDuplicates) live in diversity.go. The consensus detail surface
// is still served by the v1 ConsensusService (consensus.go); its rebuilt
// counterpart was not yet wired and is a separate cutover.
package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"

	"github.com/google/uuid"
)

// defaultProviderTimeout bounds a single provider fan-out call. Principled SLA
// choice (kept per the constants ledger); a provider may override via an
// optional SearchTimeout() method.
const defaultProviderTimeout = 1500 * time.Millisecond

// Service is the orchestrator for the rebuilt pipeline:
// Layer 0 intent → Layer 1 fan-out → Layer 2 merge → Layer 3 rank, then the
// orthogonal enrichment carried forward from v1 (artist-dedup, disambiguation,
// artwork, correction/suggest, related groups, telemetry, vocabulary learning).
type Service struct {
	providers       []ports.SearchProvider
	circuitBreaker  *CircuitBreaker
	historyRepo     ports.SearchHistoryRepository
	vocabStore      ports.VocabularyStore
	eventStore      ports.EventStore
	artworkResolver ports.ArtworkResolver
	artworkCache    ports.ArtworkCache
	albumValidator  ports.AlbumValidator
	correctionSvc   *CorrectionService
	findRelatedSvc  *FindRelatedService
	bgWg            sync.WaitGroup
}

// SearchOutput is the result envelope returned by the search use case and
// mapped to the wire by the handler.
type SearchOutput struct {
	Results          []domain.SearchResult
	ProviderStatuses []domain.ProviderSearchResponse
	Partial          bool
	CorrectedQuery   string
	OriginalQuery    string
	SuggestedQuery   string
	Related          []domain.RelatedGroup
}

// Option configures optional Service dependencies.
type Option func(*Service)

// WithHistoryRepository persists search history (best-effort).
func WithHistoryRepository(r ports.SearchHistoryRepository) Option {
	return func(s *Service) { s.historyRepo = r }
}

// WithVocabularyStore enables Layer-0 structured-intent detection, query
// correction, and background vocabulary learning.
func WithVocabularyStore(v ports.VocabularyStore) Option {
	return func(s *Service) { s.vocabStore = v }
}

// WithEventStore enables async best-effort search telemetry emission.
func WithEventStore(e ports.EventStore) Option {
	return func(s *Service) { s.eventStore = e }
}

// WithArtworkResolver enables artwork enrichment via the chained resolver.
func WithArtworkResolver(r ports.ArtworkResolver) Option {
	return func(s *Service) { s.artworkResolver = r }
}

// WithArtworkCache memoizes resolved artwork across searches.
func WithArtworkCache(c ports.ArtworkCache) Option {
	return func(s *Service) { s.artworkCache = c }
}

// WithAlbumValidator enables artist disambiguation (MusicBrainz identity).
func WithAlbumValidator(v ports.AlbumValidator) Option {
	return func(s *Service) { s.albumValidator = v }
}

// WithFindRelatedService attaches the "more from this album/artist" groups.
func WithFindRelatedService(r *FindRelatedService) Option {
	return func(s *Service) { s.findRelatedSvc = r }
}

// NewService constructs the rebuilt search orchestrator.
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

// Execute runs the rebuilt search pipeline. It mirrors the legacy
// SearchMusicService.Execute contract so the handler routes either pipeline
// through one response mapping (response parity by construction).
func (s *Service) Execute(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	saveHistory bool,
) (*SearchOutput, error) {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)

	slog.InfoContext(ctx, "search.v2.start", "query", query.Raw)

	perProvider, statuses := s.fanOut(ctx, searchQuery, query.Kinds)
	ranked := s.mergeRankEnrich(ctx, perProvider, queryNorm)

	// Zero results → auto-correct and re-search. (The "did you mean" suggestion
	// for weak-but-non-empty results was removed: its trigger was a tuned
	// relevance threshold — query-fit.)
	var correctedQuery, originalQuery, suggestedQuery string
	if len(ranked) == 0 {
		correctedQuery, originalQuery, ranked = s.tryCorrection(ctx, query)
	}

	var related []domain.RelatedGroup
	if s.findRelatedSvc != nil && len(ranked) > 0 {
		related = s.findRelatedSvc.Execute(ctx, ranked)
	}

	if len(ranked) > query.Limit {
		ranked = ranked[:query.Limit]
	}

	partial := false
	for _, st := range statuses {
		if st.Status != domain.ProviderStatusOK {
			partial = true
			break
		}
	}

	s.persistHistory(ctx, userId, query, queryNorm, saveHistory)
	s.emitSearchEvent(ctx, userId, queryNorm, ranked)
	ingestQuery := query.Raw
	if correctedQuery != "" {
		ingestQuery = correctedQuery
	}
	s.ingestVocabulary(ctx, ingestQuery, ranked)

	slog.InfoContext(ctx, "search.v2.complete",
		"query", query.Raw,
		"results", len(ranked),
		"partial", partial,
		"corrected", correctedQuery,
		"suggested", suggestedQuery,
		"related_groups", len(related),
	)

	return &SearchOutput{
		Results:          ranked,
		ProviderStatuses: statuses,
		Partial:          partial,
		CorrectedQuery:   correctedQuery,
		OriginalQuery:    originalQuery,
		SuggestedQuery:   suggestedQuery,
		Related:          related,
	}, nil
}

// mergeRankEnrich is the decision core plus carried-forward enrichment:
// Merge → Rank → diversity → artist-dedup → disambiguation → artwork.
func (s *Service) mergeRankEnrich(
	ctx context.Context,
	perProvider [][]domain.SearchResult,
	queryNorm string,
) []domain.SearchResult {
	entities := Merge(perProvider)
	ranked := Rank(entities, queryNorm)
	ranked = EnforceDiversity(ranked)
	ranked = CollapseArtistDuplicates(ranked)
	ranked = s.applyArtistDisambiguation(ctx, ranked)
	ranked = s.enrich(ctx, ranked)
	return ranked
}

// fanOut queries every provider in parallel, each bounded by a timeout and
// gated by the circuit breaker, and returns the per-provider result groups
// (for merge) plus the per-provider statuses (for the wire).
func (s *Service) fanOut(
	ctx context.Context,
	searchQuery string,
	kinds map[domain.ResultKind]bool,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	var (
		mu          sync.Mutex
		perProvider [][]domain.SearchResult
		statuses    []domain.ProviderSearchResponse
		wg          sync.WaitGroup
	)

	for _, provider := range s.providers {
		if !s.circuitBreaker.AllowRequest(provider.Name()) {
			mu.Lock()
			statuses = append(statuses, domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(p ports.SearchProvider) {
			defer wg.Done()

			timeout := defaultProviderTimeout
			if tp, ok := p.(interface{ SearchTimeout() time.Duration }); ok {
				timeout = tp.SearchTimeout()
			}
			provCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			start := time.Now()
			results, err := p.Search(provCtx, searchQuery, kinds)
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
				slog.WarnContext(ctx, "search.v2.provider_failed",
					"provider", p.Name().String(), "status", status.String(), "error", err)
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
		}(provider)
	}

	wg.Wait()
	return perProvider, statuses
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
	}
}

// WaitForBackground blocks until all best-effort background work (telemetry
// emission, vocabulary ingest) finishes. The composition root calls it on
// graceful shutdown; tests call it to observe background effects deterministically.
func (s *Service) WaitForBackground() {
	s.bgWg.Wait()
}
