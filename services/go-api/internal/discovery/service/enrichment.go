package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// EnrichmentService is the detail-open MusicBrainz enrichment use case: resolve
// the entity's MBID (passed, or strict name-resolved), look up its curated
// genres / rating / types / cross-provider ids, and resolve an HD cover via the
// existing artwork chain — read-through cached by MBID. Off the ranking path.
//
// All external calls are best-effort: a resolve/lookup failure degrades to an
// empty enrichment and a nil error (the endpoint always answers 200), never a
// surfaced error. See docs/specs/musicbrainz-enrichment/spec.md.
type EnrichmentService struct {
	enricher  ports.MetadataEnricher
	artwork   ports.ArtworkResolver
	cache     ports.EnrichmentCache
	mbidIndex ports.MBIDIndex
}

// EnrichmentOption configures optional EnrichmentService dependencies.
type EnrichmentOption func(*EnrichmentService)

// WithMBIDMemo memoizes each strict name resolution as (kind, nameKey) → mbid,
// so the search path can attach the MBID to a non-MB result later (cap 5 warm).
func WithMBIDMemo(idx ports.MBIDIndex) EnrichmentOption {
	return func(s *EnrichmentService) { s.mbidIndex = idx }
}

// NewEnrichmentService wires the enricher (required) with an optional artwork
// resolver and cache. nil artwork/cache are tolerated (no-op).
func NewEnrichmentService(
	enricher ports.MetadataEnricher,
	artwork ports.ArtworkResolver,
	cache ports.EnrichmentCache,
	opts ...EnrichmentOption,
) *EnrichmentService {
	s := &EnrichmentService{enricher: enricher, artwork: artwork, cache: cache}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Execute returns the enrichment for one entity. mbidParam wins when present
// (MB-sourced results already carry it); otherwise the MBID is name-resolved and
// a miss is negatively cached so it is not re-resolved.
func (s *EnrichmentService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbidParam string,
) (domain.MBEnrichment, error) {
	if s.enricher == nil {
		return domain.EmptyEnrichment(), nil
	}

	mbid := mbidParam
	if mbid == "" {
		resolved, ok := s.resolveMBID(ctx, kind, title, subtitle)
		if !ok {
			return domain.EmptyEnrichment(), nil
		}
		mbid = resolved
	}

	if s.cache != nil {
		if cached, found, _ := s.cache.Get(ctx, kind, mbid); found {
			return cached, nil
		}
	}

	e, err := s.enricher.Lookup(ctx, kind, mbid)
	if err != nil {
		slog.WarnContext(ctx, "enrichment.lookup_failed",
			"kind", kind.String(), "mbid", mbid, "error", err)
		return domain.EmptyEnrichment(), nil // best-effort; don't poison the cache
	}

	if s.artwork != nil {
		if url, _ := s.artwork.Resolve(ctx, kind, title, subtitle, mbid); url != "" {
			e.ArtworkURL = url
		}
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, kind, mbid, e)
	}
	return e, nil
}

// resolveMBID returns (mbid, true) when an entity is resolved, or ("", false)
// when there is nothing to enrich (no match, or a resolve error). A miss is
// negatively cached on the name key so repeats skip the MB round-trip.
func (s *EnrichmentService) resolveMBID(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle string,
) (string, bool) {
	nameKey := enrichmentNameKey(title, subtitle)

	if s.cache != nil {
		if negative, _ := s.cache.GetNegative(ctx, kind, nameKey); negative {
			return "", false
		}
	}

	resolved, err := s.enricher.ResolveMBID(ctx, kind, title, subtitle)
	if err != nil {
		slog.WarnContext(ctx, "enrichment.resolve_failed",
			"kind", kind.String(), "title", title, "error", err)
		return "", false // degrade; not cached negative (transient error, not a real miss)
	}
	if resolved == "" {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, kind, nameKey)
		}
		return "", false
	}
	if s.mbidIndex != nil {
		_ = s.mbidIndex.RememberMBID(ctx, kind, nameKey, resolved) // warms cap 5
	}
	return resolved, true
}

// enrichmentNameKey is the normalized cache key for the unresolved path. It pins
// normalization in the service so the key the cache hashes is consistent.
func enrichmentNameKey(title, subtitle string) string {
	return textnorm.NormalizeForMatch(strings.TrimSpace(title) + " " + strings.TrimSpace(subtitle))
}
