package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"log/slog"
	"time"
)

const artworkMergeBudget = 20 * time.Second

type mbidOrigin bool

const (
	mbidFromCaller          mbidOrigin = false
	mbidFromTitleResolution mbidOrigin = true
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

	if mbidParam != "" {
		return s.lookup(ctx, kind, title, subtitle, mbidParam, mbidFromCaller)
	}

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
			e, err := s.lookup(ctx, kind, title, subtitle, mbid, mbidFromTitleResolution)
			if err != nil {
				return e, false, err
			}
			return e, true, nil
		})
}

func (s *EnrichmentService) lookup(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbid string,
	origin mbidOrigin,
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

	artworkErr := s.mergeArtwork(ctx, &e, kind, title, subtitle, mbid, origin)
	if ports.IsUnverifiedArtworkMiss(e.ArtworkURL, artworkErr) {
		slog.WarnContext(ctx, "enrichment.not_cached_degraded",
			"kind", kind.String(), "mbid", mbid, "error", artworkErr)
		return e, nil
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, kind, mbid, e)
	}
	return e, nil
}

func (s *EnrichmentService) mergeArtwork(
	ctx context.Context,
	e *domain.MBEnrichment,
	kind domain.ResultKind,
	title, subtitle, mbid string,
	origin mbidOrigin,
) error {
	if s.artwork == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, artworkMergeBudget)
	defer cancel()

	id := ports.ArtworkIdentity{MBID: mbid, ExternalIDs: e.ExternalIDs}
	idURL, _, idErr := s.artwork.ResolveWithIdentityTagged(ctx, kind, title, subtitle, id)
	if idURL != "" {
		e.ArtworkURL = idURL
		return nil
	}
	if origin == mbidFromCaller {
		return idErr
	}
	nameURL, _, nameErr := s.artwork.ResolveTagged(ctx, kind, title, subtitle, mbid)
	if nameURL == "" {
		if nameErr == nil {
			return nil
		}
		return errors.Join(idErr, nameErr)
	}
	e.ArtworkURL = nameURL
	return nil
}

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
