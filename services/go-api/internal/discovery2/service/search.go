// Package service is the rebuilt discovery search pipeline — the strangler-fig
// replacement for internal/discovery/service, grown layer by layer behind the
// existing handler and gated at every step by the top-K eval (plan 003).
//
// Design doctrine: zero arbitrary, query-fit constants. Continuous/multi-signal
// judgments become categorical, structural decisions (identifier-first merge,
// version-marker categories, lexicographic relevance tiers) instead of tuned
// thresholds. Surviving numbers must be principled (SLA timeouts, RRF k=60),
// learned-later (the Layer-3 ML seam), or a single documented last resort the
// eval proves generalizes.
//
// This package REUSES the discovery context verbatim — domain value objects,
// ports, and provider adapters are imported from internal/discovery, never
// duplicated. Only the decision logic (merge, rank) is redesigned.
//
// AIDEV-NOTE: Provisional package name `discovery2`. After the rebuild runs in
// production on every surface, the old package is removed and this one is
// renamed back to `discovery` (deferred follow-up, user-decided). The dependency
// on `legacy` (the old service package, for the shared SearchOutput shape)
// disappears at that rename.
package service

import (
	"context"
	"errors"

	"altune/go-api/internal/discovery/domain"
	legacy "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
)

// Service is the slim orchestrator for the rebuilt pipeline:
// Layer 1 fan-out → Layer 2 merge → Layer 3 rank.
type Service struct{}

// NewService constructs the rebuilt search orchestrator.
//
// AIDEV-NOTE: U1 skeleton. Provider fan-out (U4), the merge cascade (U2), and
// the tier ranker (U3) attach their dependencies here as they land; the
// signature grows with them.
func NewService() *Service {
	return &Service{}
}

// Execute runs the rebuilt search pipeline. It mirrors the legacy
// SearchMusicService.Execute contract so the handler routes either pipeline
// through one response mapping (response parity by construction).
//
// AIDEV-NOTE: Not yet implemented (plan 003 U1). The decision core (U2 merge,
// U3 rank) and the orchestrator (U4 fan-out) land before this returns real
// results; until then the handler's per-surface switch keeps search on the
// legacy pipeline. This method is unreachable in production until cutover (U8).
func (s *Service) Execute(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	saveHistory bool,
) (*legacy.SearchOutput, error) {
	return nil, errors.New("discovery2: search pipeline not yet implemented (plan 003 U2–U4)")
}
