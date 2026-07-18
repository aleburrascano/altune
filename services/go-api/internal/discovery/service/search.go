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

// defaultProviderTimeout bounds a single provider fan-out call. Principled SLA
// choice (kept per the constants ledger); a provider may override via an
// optional SearchTimeout() method.
const defaultProviderTimeout = 1500 * time.Millisecond

// historyRingSize caps how many search-history rows are retained per user. An
// operational bound (like a page size), trimmed best-effort after each insert.
const historyRingSize = 100

// rankingExperiments groups the ranking-pipeline flags a Service can have
// toggled on independently (each eval A/B-gated at the composition root the
// same way): tail-noise demotion, the cross-kind prominence tiebreak,
// behavioral satisfaction scoring, and result-order exploration. Grouped so
// "what experiments exist on this Service" is one type to read instead of
// fields scattered across the struct — embedded (not a named sub-field) so
// every existing s.tailDemotion-shaped access keeps working unchanged.
type rankingExperiments struct {
	tailDemotion bool

	// crossKindProminence, default off, gates the cross-kind prominence tiebreak:
	// among equally relevant results of different kinds, the more prominent entity
	// (Deezer nb_fan/rank, log-compressed) sorts first. Fixes bare-name artist-
	// intent burial without touching track-vs-track order. eval A/B-gated exactly
	// like tailDemotion (CROSS_KIND_PROMINENCE_ENABLED).
	crossKindProminence bool

	// behavioralRanking, default off, gates the EventConsumer-derived satisfaction
	// signal as a within-tie ranking input. behavioralScores is the published
	// snapshot (atomic, refreshed off the request path); behavioralConsumer is its
	// source. Exactly the tail-demotion shape: inert until eval A/B-gated on.
	behavioralRanking  bool
	behavioralConsumer ports.EventConsumer
	behavioralScores   atomic.Pointer[map[string]float64]

	// explorationRate, 0 = off, is the fraction of searches whose result order is
	// randomized (and logged as exploration) for unbiased propensity data — the
	// small-scale bandit substitute for IPS. The one user-facing behavior change,
	// shipped behind a flag so it needs no live sign-off.
	explorationRate float64
}

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
	artworkResolver ports.TaggingArtworkResolver
	artworkCache    ports.ArtworkCache
	albumValidator  ports.AlbumValidator
	identityBridge  ports.IdentityBridge
	mbidIndex       ports.MBIDIndex
	identityStore   ports.IdentityStore
	correctionSvc   *CorrectionService
	findRelatedSvc  *FindRelatedService
	resultCache     ports.ResultCache

	rankingExperiments

	bgWg sync.WaitGroup
}

// SearchOutput is the result envelope returned by the search use case and
// mapped to the wire by the handler.
type SearchOutput struct {
	// SearchId is the keystone minted per search_performed and returned to the
	// client, which threads it back onto every downstream engagement event so the
	// impression/click/play funnel joins to the search that produced it.
	SearchId string
	// Explored is true when this search served a randomized (exploration) order.
	Explored         bool
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
func WithArtworkResolver(r ports.TaggingArtworkResolver) Option {
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

// WithIdentityStore attaches the durable reverse identity map. When MusicBrainz is
// present in a fan-out, learned bridges are persisted; on later MB-absent searches
// a provider-only result resolves its identity from here, keeping artwork
// identity-first instead of falling back to a name guess. Off → identity is only
// as good as the current fan-out (the prior, non-deterministic behavior).
func WithIdentityStore(store ports.IdentityStore) Option {
	return func(s *Service) { s.identityStore = store }
}

// WithFindRelatedService attaches the "more from this album/artist" groups.
func WithFindRelatedService(r *FindRelatedService) Option {
	return func(s *Service) { s.findRelatedSvc = r }
}

// WithResultCache enables the app-wide consistency cache (shared, short-TTL) so an
// identical query returns the identical ranked list for everyone within the window.
// Off → every search recomputes (the prior behavior).
func WithResultCache(c ports.ResultCache) Option {
	return func(s *Service) { s.resultCache = c }
}

// WithTailDemotion enables the experimental tail-noise demotion: single-source
// UGC/scrobble results with no identity (see isLowConfidenceTail) sort below every
// corroborated result. Off by default; flipped on via TAIL_DEMOTION_ENABLED for
// eval A/B. See docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md.
func WithTailDemotion() Option {
	return func(s *Service) { s.tailDemotion = true }
}

// WithCrossKindProminence enables the experimental cross-kind prominence
// tiebreak: among equally relevant results of different kinds, the more prominent
// entity sorts first (see rankWithProminence). Off by default; the composition
// root applies it under CROSS_KIND_PROMINENCE_ENABLED, eval A/B-gated like tail
// demotion. Track-vs-track order is never affected.
func WithCrossKindProminence() Option {
	return func(s *Service) { s.crossKindProminence = true }
}

// WithBehavioralRanking enables the EventConsumer-derived satisfaction signal as
// a within-tie ranking input, sourced from the given consumer. Off by default;
// the composition root applies it only under BEHAVIORAL_RANKING_ENABLED, eval
// A/B-gated exactly like tail demotion. The score map starts empty (inert) until
// the first RefreshBehavioralScores; the caller drives refresh via
// StartBehavioralRefresh.
func WithBehavioralRanking(consumer ports.EventConsumer) Option {
	return func(s *Service) {
		s.behavioralRanking = true
		s.behavioralConsumer = consumer
	}
}

// WithExploration enables exploration randomization: a `rate` fraction of
// searches (e.g. 0.03) have their result order shuffled and the search stamped
// as exploration, generating the unbiased propensity data offline counterfactual
// eval needs. Off by default (rate 0); gated by EXPLORATION_ENABLED at the
// composition root — the one user-facing change, shipped dark.
func WithExploration(rate float64) Option {
	return func(s *Service) {
		if rate > 0 {
			s.explorationRate = rate
		}
	}
}

// maybeExplore returns a possibly-randomized copy of ranked plus whether this
// search was selected for exploration. It clones before shuffling so a cached
// (shared) result list is never mutated. Inert when the rate is 0.
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

	// Mint the keystone once per search. Returned to the client and stamped on the
	// search_performed event so every downstream engagement event can join back.
	searchId := uuid.New().String()

	slog.InfoContext(ctx, "search.v2.start", "query", query.Raw)

	// App-wide consistency cache: an identical query returns the identical ranked
	// list for everyone within the TTL, smoothing provider-drop-out / cache-warmth
	// variance. Cache key is query-only (catalog-derived results are not
	// user-specific); the cached value is the full pre-limit list, re-truncated per
	// request. Inert when no cache is wired — the default path is unchanged.
	cacheKey := resultCacheKey(queryNorm, query.Kinds)
	var (
		ranked         []domain.SearchResult
		statuses       []domain.ProviderSearchResponse
		correctedQuery string
		originalQuery  string
		cached         bool
		partial        bool
	)
	if s.resultCache != nil {
		if hit, ok := s.resultCache.Get(ctx, cacheKey); ok {
			ranked, cached = hit, true
		}
	}

	if !cached {
		var perProvider [][]domain.SearchResult
		perProvider, statuses = s.fanOut(ctx, searchQuery, query.Kinds)
		ranked = s.mergeRankEnrich(ctx, perProvider, queryNorm)

		// Zero results → auto-correct and re-search. (The "did you mean" suggestion
		// for weak-but-non-empty results was removed: its trigger was a tuned
		// relevance threshold — query-fit.)
		if len(ranked) == 0 {
			correctedQuery, originalQuery, ranked = s.tryCorrection(ctx, query)
		}

		partial = anyProviderFailed(statuses)
		// Cache only complete, non-empty results: a partial (provider-drop-out) run
		// frozen for the TTL would serve a degraded list to everyone.
		if s.resultCache != nil && len(ranked) > 0 && !partial {
			s.resultCache.Set(ctx, cacheKey, ranked)
		}
	}

	var related []domain.RelatedGroup
	if s.findRelatedSvc != nil && len(ranked) > 0 {
		related = s.findRelatedSvc.Execute(ctx, ranked)
	}

	if len(ranked) > query.Limit {
		ranked = ranked[:query.Limit]
	}

	// Exploration: a small fraction of searches serve a randomized order (cloned
	// so the cache is never mutated) and are logged as exploration for propensity.
	ranked, explored := s.maybeExplore(ranked)

	s.persistHistory(ctx, userId, query, queryNorm, saveHistory)
	s.emitSearchEvent(ctx, userId, searchId, queryNorm, ranked, explored)
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
		"cached", cached,
		"tail_noise_top5", TailNoiseInTopK(ranked, 5),
	)

	return &SearchOutput{
		SearchId:         searchId,
		Explored:         explored,
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
//	display enrich   : applyArtistDisambiguation, fillArtwork (fill subtitle/
//	                   artwork; neither reorders)
func (s *Service) mergeRankEnrich(
	ctx context.Context,
	perProvider [][]domain.SearchResult,
	queryNorm string,
) []domain.SearchResult {
	// port-bound pre-merge: stamp bridged cross-provider ids (reads identity bridge).
	s.stampIdentities(ctx, perProvider)

	// pure decision core: merge → rank → list-shaping (no ports, no I/O).
	ranked := rankPipelineWith(perProvider, queryNorm, RankOptions{
		TailDemotion:        s.tailDemotion,
		CrossKindProminence: s.crossKindProminence,
		// Behavioral satisfaction signal: a published snapshot (nil when the flag
		// is off / not yet refreshed), read without locking and applied as a
		// within-tie rank input only.
		Behavioral: s.BehavioralScoresSnapshot(),
	})

	// port-bound display enrichment — fills fields without reordering.
	ranked = s.applyArtistDisambiguation(ctx, ranked)
	ranked = s.fillArtwork(ctx, ranked)
	return ranked
}

// stampIdentities annotates MB-sourced results with their bridged cross-provider
// ids (Deezer/Spotify/..., set on Xref), read from the IdentityBridge (the
// enrichment cache), so Merge can resolve identity across providers instead of by
// name alone. It is a cache-only read — no MB round-trip on the search path — and
// a no-op when the bridge is unset or an entity was never enriched. Mutates
// perProvider in place.
func (s *Service) stampIdentities(ctx context.Context, perProvider [][]domain.SearchResult) {
	if s.identityBridge == nil {
		return
	}
	// Bridges learned this fan-out (MB present) are persisted to the durable
	// identity store so a later MB-absent search can resolve the same entity.
	type learnedBridge struct {
		kind domain.ResultKind
		mbid string
		ids  map[string]string
	}
	var learned []learnedBridge

	for gi := range perProvider {
		for ri := range perProvider[gi] {
			r := &perProvider[gi][ri]
			if r.MBID == "" {
				continue
			}
			ids, ok := s.identityBridge.ExternalIDs(ctx, r.Kind, r.MBID)
			if !ok {
				continue
			}
			r.Xref = ids
			slog.DebugContext(ctx, "merge.identity_bridge_stamped",
				"kind", r.Kind.String(), "mbid", r.MBID, "ids", len(ids))
			if s.identityStore != nil {
				learned = append(learned, learnedBridge{kind: r.Kind, mbid: r.MBID, ids: ids})
			}
		}
	}

	if len(learned) == 0 {
		return
	}
	// Persist off the request path: the durable write must not add latency to the
	// search, and it must outlive the request, so use a detached context.
	bgCtx := context.WithoutCancel(ctx)
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		for _, b := range learned {
			if err := s.identityStore.PersistBridges(bgCtx, b.kind, b.mbid, b.ids); err != nil {
				slog.WarnContext(bgCtx, "identity.persist_failed",
					"kind", b.kind.String(), "mbid", b.mbid, "error", err)
			}
		}
	}()
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

// InspectSearch runs the full live pipeline (fan-out → merge → rank → enrich) for
// an operator query, bypassing the app-wide result cache so every call exercises
// live providers and artwork/identity resolution (and warms the durable identity
// store, exactly like a real search). Diagnostic only — writes no history or
// telemetry. Powers the Mission Control test-search.
func (s *Service) InspectSearch(ctx context.Context, query *domain.SearchQuery) []domain.SearchResult {
	searchQuery := CleanQuery(query.Raw)
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	perProvider, _ := s.fanOut(ctx, searchQuery, query.Kinds)
	ranked := s.mergeRankEnrich(ctx, perProvider, queryNorm)
	if query.Limit > 0 && len(ranked) > query.Limit {
		ranked = ranked[:query.Limit]
	}
	return ranked
}

// WaitForBackground blocks until all best-effort background work (telemetry
// emission, vocabulary ingest) finishes. The composition root calls it on
// graceful shutdown; tests call it to observe background effects deterministically.
func (s *Service) WaitForBackground() {
	s.bgWg.Wait()
}

// resultCacheKey builds the app-wide consistency-cache key from the normalized
// query and the requested kinds (sorted for stability). Limit is deliberately
// excluded: the cached value is the full pre-limit list, re-truncated per request,
// so different limits share one entry.
func resultCacheKey(queryNorm string, kinds map[domain.ResultKind]bool) string {
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k.String())
	}
	sort.Strings(ks)
	return queryNorm + "|" + strings.Join(ks, ",")
}

func anyProviderFailed(statuses []domain.ProviderSearchResponse) bool {
	for _, st := range statuses {
		if st.Status != domain.ProviderStatusOK {
			return true
		}
	}
	return false
}
