package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
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
		return domain.EmptyEnrichment(), nil
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
		return "", false
	}
	if resolved == "" {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, kind, nameKey)
		}
		return "", false
	}
	if s.mbidIndex != nil {
		_ = s.mbidIndex.RememberMBID(ctx, kind, nameKey, resolved)
	}
	return resolved, true
}

func enrichmentNameKey(title, subtitle string) string {
	return textnorm.NormalizeForMatch(strings.TrimSpace(title) + " " + strings.TrimSpace(subtitle))
}
