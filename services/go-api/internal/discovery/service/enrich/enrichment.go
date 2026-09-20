package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
)

type EnrichmentService struct {
	enricher  ports.MetadataEnricher
	artwork   ports.TaggingArtworkResolver
	cache     ports.EnrichmentCache
	mbidIndex ports.MBIDIndex
}

type EnrichmentOption func(*EnrichmentService)

func WithMBIDMemo(idx ports.MBIDIndex) EnrichmentOption {
	return func(s *EnrichmentService) { s.mbidIndex = idx }
}

func NewEnrichmentService(
	enricher ports.MetadataEnricher,
	artwork ports.TaggingArtworkResolver,
	cache ports.EnrichmentCache,
	opts ...EnrichmentOption,
) *EnrichmentService {
	s := &EnrichmentService{enricher: enricher, artwork: artwork, cache: cache}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *EnrichmentService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbidParam string,
) (domain.MBEnrichment, error) {
	if s.enricher == nil {
		return domain.EmptyEnrichment(), nil
	}

	// A caller-supplied MBID skips resolution (and its name-keyed negative memo)
	// and goes straight to the MBID-keyed lookup+cache.
	if mbidParam != "" {
		return s.lookup(ctx, kind, title, subtitle, mbidParam)
	}

	// The resolution step reuses the shared CachedLookup for its name-keyed
	// negative memo and degrade-to-empty (a fetch error surfaces as ErrDegraded). The positive entry is keyed by the
	// resolved MBID, so it is written inside the fetch (see lookup), not by
	// CachedLookup; mbResolutionMemo keeps Get/Set inert for that reason.
	nameKey := textnorm.NameKey(title, subtitle)
	var memo ports.NameKeyedCache[domain.MBEnrichment]
	if s.cache != nil {
		memo = mbResolutionMemo{cache: s.cache, kind: kind}
	}
	return CachedLookup(ctx, memo, nameKey, domain.EmptyEnrichment(),
		func(ctx context.Context) (domain.MBEnrichment, bool, error) {
			mbid, err := s.enricher.ResolveMBID(ctx, kind, title, subtitle)
			if err != nil {
				slog.WarnContext(ctx, "enrichment.resolve_failed",
					"kind", kind.String(), "title", title, "error", err)
				return domain.EmptyEnrichment(), false, err
			}
			if mbid == "" {
				return domain.EmptyEnrichment(), false, nil
			}
			if s.mbidIndex != nil {
				_ = s.mbidIndex.RememberMBID(ctx, kind, nameKey, mbid)
			}
			e, err := s.lookup(ctx, kind, title, subtitle, mbid)
			if err != nil {
				return e, false, err
			}
			return e, true, nil
		})
}

// lookup reads the MBID-keyed positive cache, fetches the enrichment on a miss,
// merges artwork and writes the positive entry. A lookup error degrades to empty
// without caching and is reported as ErrDegraded; an empty-but-artwork-merged result is cached as-is (the MB
// enricher never negative-caches a lookup, only an unresolved name).
func (s *EnrichmentService) lookup(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbid string,
) (domain.MBEnrichment, error) {
	if s.cache != nil {
		if cached, found, _ := s.cache.Get(ctx, kind, mbid); found {
			return cached, nil
		}
	}

	e, err := s.enricher.Lookup(ctx, kind, mbid)
	if err != nil {
		slog.WarnContext(ctx, "enrichment.lookup_failed",
			"kind", kind.String(), "mbid", mbid, "error", err)
		return domain.EmptyEnrichment(), degraded(err)
	}

	if s.artwork != nil {
		if url, _, _ := s.artwork.ResolveTagged(ctx, kind, title, subtitle, mbid); url != "" {
			e.ArtworkURL = url
		}
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, kind, mbid, e)
	}
	return e, nil
}

// mbResolutionMemo adapts the kind-partitioned EnrichmentCache negative memo
// (keyed by name) onto the generic NameKeyedCache so the MusicBrainz enricher
// can share CachedLookup's negative-cache and degrade logic. The positive entry
// is keyed by the resolved MBID rather than the name, so Get/Set are inert here
// and the MBID-keyed write happens in EnrichmentService.lookup.
type mbResolutionMemo struct {
	cache ports.EnrichmentCache
	kind  domain.ResultKind
}

func (mbResolutionMemo) Get(context.Context, string) (domain.MBEnrichment, bool, error) {
	return domain.EmptyEnrichment(), false, nil
}

func (mbResolutionMemo) Set(context.Context, string, domain.MBEnrichment) error {
	return nil
}

func (m mbResolutionMemo) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	return m.cache.GetNegative(ctx, m.kind, nameKey)
}

func (m mbResolutionMemo) SetNegative(ctx context.Context, nameKey string) error {
	return m.cache.SetNegative(ctx, m.kind, nameKey)
}
