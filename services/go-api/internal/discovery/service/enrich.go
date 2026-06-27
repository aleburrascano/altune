package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

const (
	// enrichLimit caps artwork enrichment to the top N results to bound latency.
	enrichLimit = 50
	// enrichConcurrency limits parallel enrichment goroutines.
	enrichConcurrency = 8
	// enrichTimeout bounds the whole enrichment pass.
	enrichTimeout = 4 * time.Second
	// emptyArtHash is the md5 of the empty string — some providers return a
	// placeholder image whose URL embeds it; treat that as "no artwork".
	emptyArtHash = "d41d8cd98f00b204e9800998ecf8427e"
)

// enrich resolves missing artwork for the top results in parallel. Only the
// chained artwork resolver + cache are consulted (the production wiring); the
// ranking is untouched, so no re-sort is needed.
func (s *Service) enrich(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if s.artworkResolver == nil {
		return results
	}
	limit := enrichLimit
	if len(results) < limit {
		limit = len(results)
	}
	if limit == 0 {
		return results
	}

	enrichCtx, cancel := context.WithTimeout(ctx, enrichTimeout)
	defer cancel()

	top := results[:limit]
	rest := results[limit:]

	sem := make(chan struct{}, enrichConcurrency)
	var wg sync.WaitGroup
	enriched := make([]domain.SearchResult, len(top))

	for i, r := range top {
		wg.Add(1)
		go func(idx int, result domain.SearchResult) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			enriched[idx] = s.enrichOne(enrichCtx, result)
		}(i, r)
	}

	wg.Wait()
	return append(enriched, rest...)
}

// enrichOne fills a single result's MISSING artwork: a usable provider image is
// kept as-is (R5); otherwise a usable cache hit short-circuits, else resolve via
// the identity-aware chain and memoize.
func (s *Service) enrichOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	// R5: a valid (non-placeholder) provider image is identity-bound by the merge
	// (the bridged source's own photo) — keep it, don't overwrite it with a
	// lower-confidence resolved one. Only resolve when there's no usable image.
	needsArt := result.ImageURL == "" || strings.Contains(result.ImageURL, emptyArtHash)
	if !needsArt {
		// Kept the provider's own image (R5) — tag it with that provider so the
		// coverage view distinguishes "provider supplied it" from a chain resolve.
		if result.ArtworkSource == "" && len(result.Sources) > 0 {
			result.ArtworkSource = result.Sources[0].Provider.String()
		}
		return result
	}
	mbid := stringExtra(result, "mbid")
	if mbid == "" && s.mbidIndex != nil {
		// Non-MB result with no MBID: attach a cached one (warmed by detail-opens)
		// so the MBID-keyed artwork tier (CAA/Fanart) can fire on the search card.
		if m, ok := s.mbidIndex.LookupMBID(ctx, result.Kind, enrichmentNameKey(result.Title, result.Subtitle)); ok {
			mbid = m
		}
	}

	if s.artworkCache != nil {
		if cachedURL, cachedSource, found, _ := s.artworkCache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid); found {
			usable := cachedURL != "" && !strings.Contains(cachedURL, emptyArtHash)
			if usable {
				result.ImageURL = cachedURL
				result.ArtworkSource = cachedSource
				return result
			}
			// Cached miss/placeholder: only artists retry (track-image fallback).
			if result.Kind != domain.ResultKindArtist {
				return result
			}
		}
	}

	resolved, source := s.resolveArtwork(ctx, result, mbid)
	if s.artworkCache != nil {
		_ = s.artworkCache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved, source)
	}
	if resolved != "" {
		result.ImageURL = resolved
		result.ArtworkSource = source
	}
	slog.DebugContext(ctx, "artwork.enriched",
		"kind", result.Kind.String(), "source", source,
		"resolved", resolved != "", "had_mbid", mbid != "")
	return result
}

// resolveArtwork resolves artwork identity-first: when the entity carries a
// proven identity (MBID + bridged provider ids), the identity-aware chain fetches
// the exact entity's image before any name search — the only way to get the right
// face for a same-name artist. Falls back to the name chain (no identity), then a
// same-name track image for artists whose direct lookup returns nothing.
func (s *Service) resolveArtwork(ctx context.Context, result domain.SearchResult, mbid string) (string, string) {
	identity := artworkIdentity(result, mbid)

	// Tagged path (production chain): also reports which source supplied the URL.
	if tagger, ok := s.artworkResolver.(ports.TaggingArtworkResolver); ok {
		if identity.HasLinks() {
			if url, src, _ := tagger.ResolveWithIdentityTagged(ctx, result.Kind, result.Title, result.Subtitle, identity); url != "" {
				return url, src
			}
		} else if url, src, _ := tagger.ResolveTagged(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
			return url, src
		}
		if result.Kind == domain.ResultKindArtist {
			if url, src, _ := tagger.ResolveTagged(ctx, domain.ResultKindTrack, result.Title, "", ""); url != "" {
				return url, src
			}
		}
		return "", ""
	}

	// Untagged fallback (a resolver without tagging, e.g. test fakes): no source.
	if aware, ok := s.artworkResolver.(ports.IdentityAwareArtworkResolver); ok && identity.HasLinks() {
		if url, _ := aware.ResolveWithIdentity(ctx, result.Kind, result.Title, result.Subtitle, identity); url != "" {
			return url, ""
		}
	} else if url, _ := s.artworkResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
		return url, ""
	}
	if result.Kind == domain.ResultKindArtist {
		if url, _ := s.artworkResolver.Resolve(ctx, domain.ResultKindTrack, result.Title, "", ""); url != "" {
			return url, ""
		}
	}
	return "", ""
}

// artworkIdentity assembles the proven cross-provider identity for artwork
// resolution: the MBID plus the bridged provider ids the merge stamped into
// extras["xref"] (MB → discogs/spotify/deezer/…).
func artworkIdentity(result domain.SearchResult, mbid string) ports.ArtworkIdentity {
	id := ports.ArtworkIdentity{MBID: mbid}
	if xref, ok := result.Extras["xref"].(map[string]string); ok && len(xref) > 0 {
		id.ExternalIDs = xref
	}
	return id
}
