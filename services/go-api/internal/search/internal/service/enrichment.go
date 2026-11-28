package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/music"
	"context"
	"errors"
	"fmt"
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

type NoArtwork struct{}

func (NoArtwork) ResolveTagged(context.Context, domain.ResultKind, string, string, string) (string, domain.ProviderKey, error) {
	return "", "", nil
}

func (NoArtwork) ResolveWithIdentityTagged(context.Context, domain.ResultKind, string, string, ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}

type NoEnrichmentCache struct{}

func (NoEnrichmentCache) Get(context.Context, domain.ResultKind, string) (domain.MusicBrainzEnrichment, bool, error) {
	return domain.EmptyEnrichment(), false, nil
}

func (NoEnrichmentCache) Set(context.Context, domain.ResultKind, string, domain.MusicBrainzEnrichment) error {
	return nil
}

func (NoEnrichmentCache) GetNegative(context.Context, domain.ResultKind, string) (bool, error) {
	return false, nil
}

func (NoEnrichmentCache) SetNegative(context.Context, domain.ResultKind, string) error {
	return nil
}

type NoDetailsMBIDIndex struct{}

func (NoDetailsMBIDIndex) LookupMBID(context.Context, domain.ResultKind, string) (string, bool) {
	return "", false
}

func (NoDetailsMBIDIndex) RememberMBID(context.Context, domain.ResultKind, string, string) error {
	return nil
}

func NewEnrichmentService(
	enricher ports.DetailsProvider,
	artwork ports.TaggingArtworkLookup,
	cache ports.EnrichmentCache,
	mbidIndex ports.MBIDIndex,
) (*DetailsService, error) {
	for name, missing := range map[string]bool{
		"enricher":  enricher == nil,
		"artwork":   artwork == nil,
		"cache":     cache == nil,
		"mbidIndex": mbidIndex == nil,
	} {
		if missing {
			return nil, fmt.Errorf("%w: %s is nil", domain.ErrDetailsCollaboratorRequired, name)
		}
	}
	return &DetailsService{enricher: enricher, artwork: artwork, cache: cache, mbidIndex: mbidIndex}, nil
}

func (s *DetailsService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle, mbidParam string,
) (domain.MusicBrainzEnrichment, error) {
	if mbidParam != "" {
		return s.lookup(ctx, kind, title, subtitle, mbidParam, mbidFromCaller)
	}

	nameKey := music.MatchKey(title, subtitle)
	return CachedLookup(ctx, mbResolutionMemo{cache: s.cache, kind: kind}, nameKey, domain.EmptyEnrichment(),
		func(ctx context.Context) (domain.MusicBrainzEnrichment, bool, error) {
			mbid, err := s.enricher.ResolveMBID(ctx, kind, title, subtitle)
			if err != nil {
				slog.WarnContext(ctx, "enrichment.resolve_failed",
					"kind", kind.String(), "title", title, logging.KeyError, err)
				return domain.EmptyEnrichment(), false, err
			}
			if mbid == "" {
				return domain.EmptyEnrichment(), false, nil
			}
			_ = s.mbidIndex.RememberMBID(ctx, kind, nameKey, mbid)
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
	if cached, found, _ := s.cache.Get(ctx, kind, mbid); found {
		return cached, nil
	}

	e, err := s.enricher.Lookup(ctx, kind, mbid)
	if err != nil {
		slog.WarnContext(ctx, "enrichment.lookup_failed",
			"kind", kind.String(), "mbid", mbid, logging.KeyError, err)
		return domain.EmptyEnrichment(), degraded(err)
	}

	artworkErr := s.mergeArtwork(ctx, &e, kind, title, subtitle, mbid, origin)
	if ports.IsUnverifiedArtworkMiss(e.ArtworkURL, artworkErr) {
		slog.WarnContext(ctx, "enrichment.not_cached_degraded",
			"kind", kind.String(), "mbid", mbid, logging.KeyError, artworkErr)
		return e, nil
	}

	if err := s.cache.Set(ctx, kind, mbid, e); err != nil {
		slog.WarnContext(ctx, "enrichment.cache_set_failed", "kind", kind.String(), "mbid", mbid, logging.KeyError, err)
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
