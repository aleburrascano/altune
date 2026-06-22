package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// LastFmEnrichmentService is the detail-open Last.fm enrichment use case: look
// up an entity's listen-based popularity, weighted tags, bio, and (for artists)
// the similar-artist graph — read-through cached by the normalized name key.
// Off the ranking path (display-only).
//
// All external calls are best-effort: a lookup failure degrades to an empty
// enrichment and a nil error (the endpoint always answers 200), never a
// surfaced error. See docs/providers/lastfm.md cap 3.
type LastFmEnrichmentService struct {
	enricher ports.LastFmEnricher
	cache    ports.LastFmEnrichmentCache
}

// NewLastFmEnrichmentService wires the enricher (required) with an optional
// cache (nil tolerated — runs uncached).
func NewLastFmEnrichmentService(
	enricher ports.LastFmEnricher,
	cache ports.LastFmEnrichmentCache,
) *LastFmEnrichmentService {
	return &LastFmEnrichmentService{enricher: enricher, cache: cache}
}

// Execute returns the Last.fm enrichment for one entity. The (kind, title,
// subtitle) wire shape is translated to Last.fm's (artist, entityTitle): for an
// artist the title IS the artist; for a track/album the subtitle is the artist
// and the title is the entity. A cache hit short-circuits the network; a
// negatively-cached or unresolved name returns empty.
func (s *LastFmEnrichmentService) Execute(
	ctx context.Context,
	kind domain.ResultKind,
	title, subtitle string,
) (domain.LastFmEnrichment, error) {
	artistName, entityTitle := lastfmLookupNames(kind, title, subtitle)
	if s.enricher == nil || strings.TrimSpace(artistName) == "" {
		return domain.EmptyLastFmEnrichment(), nil
	}

	nameKey := lastfmNameKey(kind, artistName, entityTitle)

	if s.cache != nil {
		if cached, found, _ := s.cache.Get(ctx, nameKey); found {
			return cached, nil
		}
		if negative, _ := s.cache.GetNegative(ctx, nameKey); negative {
			return domain.EmptyLastFmEnrichment(), nil
		}
	}

	e, err := s.enricher.Lookup(ctx, kind, artistName, entityTitle)
	if err != nil {
		slog.WarnContext(ctx, "lastfm_enrichment.lookup_failed",
			"kind", kind.String(), "artist", artistName, "title", entityTitle, "error", err)
		return domain.EmptyLastFmEnrichment(), nil // transient; not cached negative
	}

	if e.IsZero() {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, nameKey)
		}
		return domain.EmptyLastFmEnrichment(), nil
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, nameKey, e)
	}
	return e, nil
}

// lastfmLookupNames maps the wire (kind, title, subtitle) to Last.fm's
// (artist, entityTitle): artist detail keys on the title; track/album detail
// keys on the subtitle (artist) + title (entity).
func lastfmLookupNames(kind domain.ResultKind, title, subtitle string) (artistName, entityTitle string) {
	if kind == domain.ResultKindArtist {
		return strings.TrimSpace(title), ""
	}
	return strings.TrimSpace(subtitle), strings.TrimSpace(title)
}

// lastfmNameKey is the normalized cache key for a (kind, artist, title) lookup,
// pinned in the service so the key the cache hashes is consistent.
func lastfmNameKey(kind domain.ResultKind, artistName, entityTitle string) string {
	return NormalizeForMatch(kind.String() + " " + artistName + " " + entityTitle)
}
