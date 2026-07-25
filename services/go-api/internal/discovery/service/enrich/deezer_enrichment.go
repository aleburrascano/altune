package enrich

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type DeezerEnrichmentService struct {
	enricher ports.DeezerEnricher
	cache    ports.DeezerEnrichmentCache
}

func NewDeezerEnrichmentService(
	enricher ports.DeezerEnricher,
	cache ports.DeezerEnrichmentCache,
) *DeezerEnrichmentService {
	return &DeezerEnrichmentService{enricher: enricher, cache: cache}
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
	if s.enricher == nil || entityTitle == "" {
		return domain.EmptyDeezerEnrichment(), nil
	}

	return CachedLookup(ctx, s.cache, deezerNameKey(kind, artist, entityTitle), domain.EmptyDeezerEnrichment(),
		func(ctx context.Context) (domain.DeezerEnrichment, bool, error) {
			v, found, err := resolveThenLookup(
				ctx,
				func(ctx context.Context) (string, error) { return s.enricher.ResolveID(ctx, kind, artist, entityTitle) },
				func(ctx context.Context, id string) (domain.DeezerEnrichment, error) {
					return s.enricher.Lookup(ctx, kind, id)
				},
				domain.DeezerEnrichment.IsZero,
			)
			if err != nil {
				slog.WarnContext(ctx, "deezer_enrichment.failed",
					"kind", kind.String(), "artist", artist, "title", entityTitle, "error", err)
			}
			return v, found, err
		})
}

func deezerNameKey(kind domain.ResultKind, artist, title string) string {
	return textnorm.NormalizeForMatch(kind.String() + " " + artist + " " + title)
}
