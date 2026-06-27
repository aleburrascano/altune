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

// historyRingSize caps how many search-history rows are retained per user. An
// operational bound (like a page size), trimmed best-effort after each insert.
const historyRingSize = 100

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
	identityBridge  ports.IdentityBridge
	mbidIndex       ports.MBIDIndex
	correctionSvc   *CorrectionService
	findRelatedSvc  *FindRelatedService
	tailDemotion    bool
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

// WithIdentityBridge enables cross-provider identity merging: MB results are
// stamped with their bridged provider ids (from the enrichment cache) before
// merge, so a result merges by stated identity, not just name. Off → name-only
// merge (the prior behavior).
func WithIdentityBridge(b ports.IdentityBridge) Option {
	return func(s *Service) { s.identityBridge = b }
}

// WithMBIDIndex lets search-card artwork attach a cached MBID to a non-MB result
// (name→mbid memo warmed by detail-opens), so the MBID-keyed artwork tier fires
// on the list too. Off → non-MB results keep their provider thumbnail.
func WithMBIDIndex(idx ports.MBIDIndex) Option {
	return func(s *Service) { s.mbidIndex = idx }
}

// WithFindRelatedService attaches the "more from this album/artist" groups.
func WithFindRelatedService(r *FindRelatedService) Option {
	return func(s *Service) { s.findRelatedSvc = r }
}

// WithTailDemotion enables the experimental tail-noise demotion: single-source
// UGC/scrobble results with no identity (see isLowConfidenceTail) sort below every
// corroborated result. Off by default; flipped on via TAIL_DEMOTION_ENABLED for
// eval A/B. See docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md.
func WithTailDemotion() Option {
	return func(s *Service) { s.tailDemotion = true }
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
	var correctedQuery, originalQuery string
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
		"related_groups", len(related),
	)

	return &SearchOutput{
		Results:          ranked,
		ProviderStatuses: statuses,
		Partial:          partial,
		CorrectedQuery:   correctedQuery,
		OriginalQuery:    originalQuery,
		Related:          related,
	}, nil
}

// mergeRankEnrich is the decision core followed by two carried-forward concerns
// that each own one responsibility and are tested in isolation:
//
//	decision core   : Merge (entity resolution) → Rank (relevance ordering)
//	list policy      : EnforceDiversity, CollapseArtistDuplicates (product rules
//	                   that reshape the list; see diversity.go)
//	display enrich   : applyArtistDisambiguation, enrich (fill subtitle/artwork;
//	                   neither reorders)
func (s *Service) mergeRankEnrich(
	ctx context.Context,
	perProvider [][]domain.SearchResult,
	queryNorm string,
) []domain.SearchResult {
	// port-bound pre-merge: stamp bridged cross-provider ids (reads identity bridge).
	s.stampIdentities(ctx, perProvider)

	// pure decision core: merge → rank → list-shaping (no ports, no I/O).
	var demote demoteFunc
	if s.tailDemotion {
		demote = isLowConfidenceTail
	}
	ranked := rankPipelineWith(perProvider, queryNorm, demote)
	// Observability for the demotion experiment: how much low-confidence noise
	// remained in the visible top-5 after demotion (residual is the genuinely-
	// underground case where no cleaner result exists to promote). Flag-gated, so
	// zero cost on the default path. Debug level.
	if s.tailDemotion {
		noiseTop5, noiseTotal := 0, 0
		for i, r := range ranked {
			if isLowConfidenceTail(r) {
				noiseTotal++
				if i < 5 {
					noiseTop5++
				}
			}
		}
		slog.DebugContext(ctx, "search.v2.tailnoise", "query", queryNorm,
			"noise_top5", noiseTop5, "noise_total", noiseTotal, "results", len(ranked))
	}

	// port-bound display enrichment — fills fields without reordering.
	ranked = s.applyArtistDisambiguation(ctx, ranked)
	ranked = s.enrich(ctx, ranked)
	return ranked
}

// stampIdentities annotates MB-sourced results with their bridged cross-provider
// ids (Deezer/Spotify/...), read from the IdentityBridge (the enrichment cache),
// so Merge can resolve identity across providers instead of by name alone. It is
// a cache-only read — no MB round-trip on the search path — and a no-op when the
// bridge is unset or an entity was never enriched. Mutates perProvider in place.
func (s *Service) stampIdentities(ctx context.Context, perProvider [][]domain.SearchResult) {
	if s.identityBridge == nil {
		return
	}
	for gi := range perProvider {
		for ri := range perProvider[gi] {
			r := &perProvider[gi][ri]
			mbid := stringExtra(*r, "mbid")
			if mbid == "" {
				continue
			}
			ids, ok := s.identityBridge.ExternalIDs(ctx, r.Kind, mbid)
			if !ok {
				continue
			}
			if r.Extras == nil {
				r.Extras = make(map[string]any, 1)
			}
			r.Extras["xref"] = ids
			slog.DebugContext(ctx, "merge.identity_bridge_stamped",
				"kind", r.Kind.String(), "mbid", mbid, "ids", len(ids))
		}
	}
}

// fanOut queries every provider in parallel, each bounded by a timeout and
// gated by the circuit breaker, and returns the per-provider result groups
// (for merge) plus the per-provider statuses (for the wire).
//
// Both outputs are ordered by the fixed provider order (s.providers), NOT by
// goroutine-completion order. Each goroutine writes only its own slot, so the
// downstream merge/rank input — and thus the final ranking of otherwise-tied
// results — is deterministic run-to-run. Disjoint-index writes need no mutex.
func (s *Service) fanOut(
	ctx context.Context,
	searchQuery string,
	kinds map[domain.ResultKind]bool,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	results := make([][]domain.SearchResult, len(s.providers))
	statuses := make([]domain.ProviderSearchResponse, len(s.providers))
	var wg sync.WaitGroup

	for i, provider := range s.providers {
		if !s.circuitBreaker.AllowRequest(provider.Name()) {
			statuses[i] = domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			}
			continue
		}

		wg.Add(1)
		go func(i int, p ports.SearchProvider) {
			defer wg.Done()

			timeout := defaultProviderTimeout
			if tp, ok := p.(interface{ SearchTimeout() time.Duration }); ok {
				timeout = tp.SearchTimeout()
			}
			provCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			start := time.Now()
			res, err := p.Search(provCtx, searchQuery, kinds)
			latencyMs := time.Since(start).Milliseconds()

			if err != nil {
				s.circuitBreaker.RecordFailure(p.Name())
				status := domain.ProviderStatusError
				if provCtx.Err() != nil {
					status = domain.ProviderStatusTimeout
				}
				statuses[i] = domain.ProviderSearchResponse{
					Provider:  p.Name(),
					Status:    status,
					LatencyMs: latencyMs,
				}
				slog.WarnContext(ctx, "search.v2.provider_failed",
					"provider", p.Name().String(), "status", status.String(), "error", err)
				return
			}

			s.circuitBreaker.RecordSuccess(p.Name())
			results[i] = res
			statuses[i] = domain.ProviderSearchResponse{
				Provider:    p.Name(),
				Results:     res,
				Status:      domain.ProviderStatusOK,
				LatencyMs:   latencyMs,
				ResultCount: len(res),
			}
		}(i, provider)
	}

	wg.Wait()

	// Flatten to per-provider groups in fixed provider order, keeping only
	// providers that returned results (circuit-open/error/timeout slots are nil).
	perProvider := make([][]domain.SearchResult, 0, len(s.providers))
	for _, r := range results {
		if len(r) > 0 {
			perProvider = append(perProvider, r)
		}
	}
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
		return
	}
	// Ring-buffer trim: cap retained history per user so the table does not grow
	// unbounded. Best-effort — a trim failure must not fail the search.
	if err := s.historyRepo.TrimToN(ctx, userId, historyRingSize); err != nil {
		slog.WarnContext(ctx, "search.v2.history_trim_failed", "error", err)
	}
}

// RankVariantsForEval runs ONE provider fan-out and returns the ranked list
// both with and without the post-rank reshaping tier (EnforceDiversity +
// CollapseArtistDuplicates). Eval-only seam for the diversity harness (plan
// 2026-06-24-001): a single fan-out keeps provider load identical to a normal
// search — critical under provider rate limits — while exposing exactly what
// reshaping changed. It deliberately skips display enrichment (artwork,
// disambiguation), which fills fields and never reorders.
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

// WaitForBackground blocks until all best-effort background work (telemetry
// emission, vocabulary ingest) finishes. The composition root calls it on
// graceful shutdown; tests call it to observe background effects deterministically.
func (s *Service) WaitForBackground() {
	s.bgWg.Wait()
}
