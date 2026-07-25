package enrich

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type LastFmEnrichmentService struct {
	enricher ports.LastFmEnricher
	cache    ports.LastFmEnrichmentCache
}

func NewLastFmEnrichmentService(
	enricher ports.LastFmEnricher,
	cache ports.LastFmEnrichmentCache,
) *LastFmEnrichmentService {
	return &LastFmEnrichmentService{enricher: enricher, cache: cache}
}

func (s *LastFmEnrichmentService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle string,
) (domain.LastFmEnrichment, error) {
	artistName, entityTitle := lastfmLookupNames(kind, title, subtitle)
	if s.enricher == nil || strings.TrimSpace(artistName) == "" {
		return domain.EmptyLastFmEnrichment(), nil
	}

	return CachedLookup(ctx, s.cache, lastfmNameKey(kind, artistName, entityTitle), domain.EmptyLastFmEnrichment(),
		func(ctx context.Context) (domain.LastFmEnrichment, bool, error) {
			e, err := s.enricher.Lookup(ctx, kind, artistName, entityTitle)
			if err != nil {
				slog.WarnContext(ctx, "lastfm_enrichment.lookup_failed",
					"kind", kind.String(), "artist", artistName, "title", entityTitle, "error", err)
				return domain.EmptyLastFmEnrichment(), false, err
			}
			if e.IsZero() {
				return domain.EmptyLastFmEnrichment(), false, nil
			}
			return e, true, nil
		})
}

func lastfmLookupNames(kind domain.ResultKind, title, subtitle string) (artistName, entityTitle string) {
	if kind == domain.ResultKindArtist {
		return strings.TrimSpace(title), ""
	}
	return strings.TrimSpace(subtitle), strings.TrimSpace(title)
}

func lastfmNameKey(kind domain.ResultKind, artistName, entityTitle string) string {
	return textnorm.NormalizeForMatch(kind.String() + " " + artistName + " " + entityTitle)
}
