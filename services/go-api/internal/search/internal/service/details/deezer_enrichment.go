package details

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type DeezerEnrichmentService struct {
	enricher ports.DeezerEnricher
	cache    ports.DeezerEnrichmentCache
}

func NewDeezerEnrichmentService(
	enricher ports.DeezerEnricher,
	cache ports.DeezerEnrichmentCache,
) (*DeezerEnrichmentService, error) {
	for name, missing := range map[string]bool{
		"deezer enricher": enricher == nil,
		"deezer cache":    cache == nil,
	} {
		if missing {
			return nil, fmt.Errorf("%w: %s is nil", ErrCollaboratorRequired, name)
		}
	}
	return &DeezerEnrichmentService{enricher: enricher, cache: cache}, nil
}

func (s *DeezerEnrichmentService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle string,
) (domain.DeezerEnrichment, error) {
	if kind != domain.ResultKindTrack && kind != domain.ResultKindAlbum {
		return domain.EmptyDeezerEnrichment(), nil
	}
	artist := strings.TrimSpace(subtitle)
	entityTitle := strings.TrimSpace(title)
	if entityTitle == "" {
		return domain.EmptyDeezerEnrichment(), nil
	}

	return CachedLookup(ctx, s.cache, kindNameKey(kind, artist, entityTitle), domain.EmptyDeezerEnrichment(),
		func(ctx context.Context) (domain.DeezerEnrichment, bool, error) {
			id, err := s.enricher.ResolveID(ctx, kind, artist, entityTitle)
			if err != nil {
				slog.WarnContext(ctx, "deezer_enrichment.failed",
					"kind", kind.String(), "artist", artist, "title", entityTitle, logging.KeyError, err)
				return domain.EmptyDeezerEnrichment(), false, err
			}
			if id == "" {
				return domain.EmptyDeezerEnrichment(), false, nil
			}

			v, err := s.enricher.Lookup(ctx, kind, id)
			if err != nil {
				slog.WarnContext(ctx, "deezer_enrichment.failed",
					"kind", kind.String(), "artist", artist, "title", entityTitle, logging.KeyError, err)
				return domain.EmptyDeezerEnrichment(), false, err
			}
			if v.IsZero() {
				return domain.EmptyDeezerEnrichment(), false, nil
			}
			return v, true, nil
		})
}
