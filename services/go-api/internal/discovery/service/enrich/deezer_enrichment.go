package enrich

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// DeezerEnrichmentService is the detail-open Deezer enrichment use case: resolve
// the entity's Deezer id from (kind, artist, title), then look up its audio
// fields / album liner data — read-through cached by the normalized name key.
// Off the ranking path (display-only).
//
// All external calls are best-effort: a resolve/lookup failure degrades to an
// empty enrichment and a nil error (the endpoint always answers 200), never a
// surfaced error. See docs/providers/deezer.md (caps 7–8). Only track and album
// are enriched; an artist kind returns empty.
type DeezerEnrichmentService struct {
	enricher ports.DeezerEnricher
	cache    ports.DeezerEnrichmentCache
}

// NewDeezerEnrichmentService wires the enricher (required) with an optional
// cache (nil tolerated — runs uncached).
func NewDeezerEnrichmentService(
	enricher ports.DeezerEnricher,
	cache ports.DeezerEnrichmentCache,
) *DeezerEnrichmentService {
	return &DeezerEnrichmentService{enricher: enricher, cache: cache}
}

// Execute returns the Deezer enrichment for one track or album. The wire (kind,
// title, subtitle) maps to Deezer's (artist, title): the subtitle is the artist,
// the title is the entity. A cache hit short-circuits the network; a
// negatively-cached or unresolved name returns empty.
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
			return resolveThenLookup(
				ctx,
				func(ctx context.Context) (string, error) { return s.enricher.ResolveID(ctx, kind, artist, entityTitle) },
				func(ctx context.Context, id string) (domain.DeezerEnrichment, error) { return s.enricher.Lookup(ctx, kind, id) },
				domain.DeezerEnrichment.IsZero,
				func(err error) {
					slog.WarnContext(ctx, "deezer_enrichment.resolve_failed",
						"kind", kind.String(), "artist", artist, "title", entityTitle, "error", err)
				},
				func(id string, err error) {
					slog.WarnContext(ctx, "deezer_enrichment.lookup_failed",
						"kind", kind.String(), "id", id, "title", entityTitle, "error", err)
				},
			)
		})
}

// deezerNameKey is the normalized cache key for a (kind, artist, title) lookup,
// pinned in the service so the key the cache hashes is consistent.
func deezerNameKey(kind domain.ResultKind, artist, title string) string {
	return textnorm.NormalizeForMatch(kind.String() + " " + artist + " " + title)
}
