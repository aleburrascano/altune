package details

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
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

type DetailsService struct {
	enricher  ports.DetailsProvider
	artwork   ports.TaggingArtworkLookup
	cache     ports.EnrichmentCache
	mbidIndex ports.MBIDIndex
}

type EnrichmentOption func(*DetailsService)

func WithMBIDMemo(idx ports.MBIDIndex) EnrichmentOption {
	return func(s *DetailsService) { s.mbidIndex = idx }
}

func NewEnrichmentService(
	enricher ports.DetailsProvider,
	artwork ports.TaggingArtworkLookup,
	cache ports.EnrichmentCache,
	opts ...EnrichmentOption,
) *DetailsService {
	s := &DetailsService{enricher: enricher, artwork: artwork, cache: cache}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *DetailsService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbidParam string,
) (domain.MusicBrainzEnrichment, error) {
	if s.enricher == nil {
		return domain.EmptyEnrichment(), nil
	}

	if mbidParam != "" {
		return s.lookup(ctx, kind, title, subtitle, mbidParam, mbidFromCaller)
	}

	nameKey := textnorm.NameKey(title, subtitle)
	var memo ports.NameKeyedCache[domain.MusicBrainzEnrichment]
	if s.cache != nil {
		memo = mbResolutionMemo{cache: s.cache, kind: kind}
	}
	return CachedLookup(ctx, memo, nameKey, domain.EmptyEnrichment(),
		func(ctx context.Context) (domain.MusicBrainzEnrichment, bool, error) {
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

func (s *DetailsService) lookup(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbid string,
	origin mbidOrigin,
) (domain.MusicBrainzEnrichment, error) {
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

func (s *DetailsService) mergeArtwork(
	ctx context.Context,
	e *domain.MusicBrainzEnrichment,
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

func (mbResolutionMemo) Get(context.Context, string) (domain.MusicBrainzEnrichment, bool, error) {
	return domain.EmptyEnrichment(), false, nil
}

func (mbResolutionMemo) Set(context.Context, string, domain.MusicBrainzEnrichment) error {
	return nil
}

func (m mbResolutionMemo) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	return m.cache.GetNegative(ctx, m.kind, nameKey)
}

func (m mbResolutionMemo) SetNegative(ctx context.Context, nameKey string) error {
	return m.cache.SetNegative(ctx, m.kind, nameKey)
}
