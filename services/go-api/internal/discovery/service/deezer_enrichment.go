package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
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

	nameKey := deezerNameKey(kind, artist, entityTitle)

	if s.cache != nil {
		if cached, found, _ := s.cache.Get(ctx, nameKey); found {
			return cached, nil
		}
		if negative, _ := s.cache.GetNegative(ctx, nameKey); negative {
			return domain.EmptyDeezerEnrichment(), nil
		}
	}

	id, err := s.enricher.ResolveID(ctx, kind, artist, entityTitle)
	if err != nil {
		slog.WarnContext(ctx, "deezer_enrichment.resolve_failed",
			"kind", kind.String(), "artist", artist, "title", entityTitle, "error", err)
		return domain.EmptyDeezerEnrichment(), nil // transient; not cached negative
	}
	if id == "" {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, nameKey)
		}
		return domain.EmptyDeezerEnrichment(), nil
	}

	e, err := s.enricher.Lookup(ctx, kind, id)
	if err != nil {
		slog.WarnContext(ctx, "deezer_enrichment.lookup_failed",
			"kind", kind.String(), "id", id, "title", entityTitle, "error", err)
		return domain.EmptyDeezerEnrichment(), nil // best-effort; don't poison the cache
	}

	if e.IsZero() {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, nameKey)
		}
		return domain.EmptyDeezerEnrichment(), nil
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, nameKey, e)
	}
	return e, nil
}

// deezerNameKey is the normalized cache key for a (kind, artist, title) lookup,
// pinned in the service so the key the cache hashes is consistent.
func deezerNameKey(kind domain.ResultKind, artist, title string) string {
	return NormalizeForMatch(kind.String() + " " + artist + " " + title)
}
