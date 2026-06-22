package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// DiscogsEnrichmentService is the detail-open Discogs album enrichment use case:
// resolve the album's Discogs master from (artist, album), then look up its
// credits / styles / label / community — read-through cached by the normalized
// name key. Off the ranking path (display-only).
//
// All external calls are best-effort: a resolve/lookup failure degrades to an
// empty enrichment and a nil error (the endpoint always answers 200), never a
// surfaced error. See docs/providers/discogs.md (caps 3–6).
type DiscogsEnrichmentService struct {
	enricher ports.DiscogsEnricher
	cache    ports.DiscogsEnrichmentCache
}

// NewDiscogsEnrichmentService wires the enricher (required) with an optional
// cache (nil tolerated — runs uncached).
func NewDiscogsEnrichmentService(
	enricher ports.DiscogsEnricher,
	cache ports.DiscogsEnrichmentCache,
) *DiscogsEnrichmentService {
	return &DiscogsEnrichmentService{enricher: enricher, cache: cache}
}

// Execute returns the Discogs enrichment for one album. A cache hit short-circuits
// the network; a negatively-cached name returns empty without re-resolving; an
// unresolved master is negatively cached so it is not re-resolved every open.
func (s *DiscogsEnrichmentService) Execute(
	ctx context.Context,
	artist, album string,
) (domain.DiscogsEnrichment, error) {
	if s.enricher == nil || strings.TrimSpace(album) == "" {
		return domain.EmptyDiscogsEnrichment(), nil
	}

	nameKey := discogsNameKey(artist, album)

	if s.cache != nil {
		if cached, found, _ := s.cache.Get(ctx, nameKey); found {
			return cached, nil
		}
		if negative, _ := s.cache.GetNegative(ctx, nameKey); negative {
			return domain.EmptyDiscogsEnrichment(), nil
		}
	}

	masterID, err := s.enricher.ResolveMasterID(ctx, artist, album)
	if err != nil {
		slog.WarnContext(ctx, "discogs_enrichment.resolve_failed",
			"artist", artist, "album", album, "error", err)
		return domain.EmptyDiscogsEnrichment(), nil // transient; not cached negative
	}
	if masterID == 0 {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, nameKey)
		}
		return domain.EmptyDiscogsEnrichment(), nil
	}

	e, err := s.enricher.LookupAlbum(ctx, masterID)
	if err != nil {
		slog.WarnContext(ctx, "discogs_enrichment.lookup_failed",
			"master_id", masterID, "album", album, "error", err)
		return domain.EmptyDiscogsEnrichment(), nil // best-effort; don't poison the cache
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, nameKey, e)
	}
	return e, nil
}

// discogsNameKey is the normalized cache key for an (artist, album) pair, pinned
// in the service so the key the cache hashes is consistent.
func discogsNameKey(artist, album string) string {
	return NormalizeForMatch(strings.TrimSpace(artist) + " " + strings.TrimSpace(album))
}

// DiscogsArtistEnrichmentService is the detail-open Discogs artist enrichment use
// case: resolve the artist's Discogs id from its name, then look up its
// bio/aliases/groups/links — read-through cached by the normalized name. Off the
// ranking path (display-only); best-effort like its album sibling.
type DiscogsArtistEnrichmentService struct {
	enricher ports.DiscogsEnricher
	cache    ports.DiscogsArtistEnrichmentCache
}

func NewDiscogsArtistEnrichmentService(
	enricher ports.DiscogsEnricher,
	cache ports.DiscogsArtistEnrichmentCache,
) *DiscogsArtistEnrichmentService {
	return &DiscogsArtistEnrichmentService{enricher: enricher, cache: cache}
}

// Execute returns the Discogs enrichment for one artist by name. Mirrors the
// album service's cache → resolve → lookup flow, with the same best-effort
// degradation (always a nil error to the caller).
func (s *DiscogsArtistEnrichmentService) Execute(
	ctx context.Context,
	name string,
) (domain.DiscogsArtistEnrichment, error) {
	if s.enricher == nil || strings.TrimSpace(name) == "" {
		return domain.EmptyDiscogsArtistEnrichment(), nil
	}

	nameKey := NormalizeForMatch(strings.TrimSpace(name))

	if s.cache != nil {
		if cached, found, _ := s.cache.Get(ctx, nameKey); found {
			return cached, nil
		}
		if negative, _ := s.cache.GetNegative(ctx, nameKey); negative {
			return domain.EmptyDiscogsArtistEnrichment(), nil
		}
	}

	artistID, err := s.enricher.ResolveArtistID(ctx, name)
	if err != nil {
		slog.WarnContext(ctx, "discogs_artist_enrichment.resolve_failed",
			"name", name, "error", err)
		return domain.EmptyDiscogsArtistEnrichment(), nil
	}
	if artistID == 0 {
		if s.cache != nil {
			_ = s.cache.SetNegative(ctx, nameKey)
		}
		return domain.EmptyDiscogsArtistEnrichment(), nil
	}

	e, err := s.enricher.LookupArtist(ctx, artistID)
	if err != nil {
		slog.WarnContext(ctx, "discogs_artist_enrichment.lookup_failed",
			"artist_id", artistID, "name", name, "error", err)
		return domain.EmptyDiscogsArtistEnrichment(), nil
	}

	if s.cache != nil {
		_ = s.cache.Set(ctx, nameKey, e)
	}
	return e, nil
}
