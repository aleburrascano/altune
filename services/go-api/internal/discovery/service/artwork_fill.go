package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"golang.org/x/sync/errgroup"
)

const (
	// artworkFillLimit caps artwork enrichment to the top N results to bound latency.
	artworkFillLimit = 50
	// artworkFillConcurrency limits parallel enrichment goroutines.
	artworkFillConcurrency = 8
	// artworkFillTimeout bounds the whole enrichment pass.
	artworkFillTimeout = 4 * time.Second
	// emptyArtHash is the md5 of the empty string — some providers return a
	// placeholder image whose URL embeds it; treat that as "no artwork".
	emptyArtHash = "d41d8cd98f00b204e9800998ecf8427e"
)

// fillArtwork resolves missing artwork for the top results in parallel. Only the
// chained artwork resolver + cache are consulted (the production wiring); the
// ranking is untouched, so no re-sort is needed.
func (s *Service) fillArtwork(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if s.artworkResolver == nil {
		return results
	}
	limit := artworkFillLimit
	if len(results) < limit {
		limit = len(results)
	}
	if limit == 0 {
		return results
	}

	fillCtx, cancel := context.WithTimeout(ctx, artworkFillTimeout)
	defer cancel()

	top := results[:limit]
	rest := results[limit:]

	var g errgroup.Group
	g.SetLimit(artworkFillConcurrency)
	filled := make([]domain.SearchResult, len(top))

	for i, r := range top {
		g.Go(func() error {
			filled[i] = s.fillArtworkOne(fillCtx, r)
			return nil
		})
	}
	_ = g.Wait()
	return append(filled, rest...)
}

// fillArtworkOne fills a single result's MISSING artwork: a usable provider image is
// kept as-is (R5); otherwise a usable cache hit short-circuits, else resolve via
// the identity-aware chain and memoize.
func (s *Service) fillArtworkOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
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
		setArtworkPath(&result, "provider")
		return result
	}
	mbid := result.MBID
	fromDurable := false
	// Durable identity (the deterministic fix): a provider-only result (MusicBrainz
	// absent from this fan-out, so no Xref was stamped in merge) resolves its MBID +
	// bridged ids from the persisted identity store, keyed on its OWN provider id.
	// This makes artwork identity-first — the correct same-name entity — even when
	// MB never answered this search, instead of gambling on a name lookup. Skipped
	// when the merge already stamped Xref (MB was present).
	if len(result.Xref) == 0 && s.identityStore != nil && len(result.Sources) > 0 {
		src := result.Sources[0]
		if m, xref, ok := s.identityStore.LookupByProviderID(ctx, result.Kind, src.Provider.String(), src.ExternalID); ok {
			fromDurable = true
			if mbid == "" {
				mbid = m
			}
			if len(xref) > 0 {
				result.Xref = xref
			}
			// Visible only when MB was absent for this result (xref wasn't stamped in
			// merge) yet identity was recovered from the durable store — i.e. exactly
			// the deterministic-fix path firing.
			slog.DebugContext(ctx, "identity.durable_resolved",
				"kind", result.Kind.String(), "provider", src.Provider.String(),
				"external_id", src.ExternalID, "mbid", m, "bridged_ids", len(xref))
		}
	}
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
				setArtworkPath(&result, "cache")
				return result
			}
			// Cached miss/placeholder: only artists retry — a durable identity may
			// now resolve the exact entity where a prior name-only attempt missed.
			if result.Kind != domain.ResultKindArtist {
				setArtworkPath(&result, "none")
				return result
			}
		}
	}

	resolved, source, confidence := s.resolveArtwork(ctx, result, mbid)
	if s.artworkCache != nil {
		_ = s.artworkCache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved, source, confidence)
	}
	if resolved != "" {
		result.ImageURL = resolved
		result.ArtworkSource = source
	}
	setArtworkPath(&result, artworkPathFor(resolved, confidence, fromDurable))
	slog.DebugContext(ctx, "artwork.enriched",
		"kind", result.Kind.String(), "source", source,
		"resolved", resolved != "", "had_mbid", mbid != "")
	return result
}

// setArtworkPath records, in extras, HOW a result's artwork was resolved
// (provider / cache / identity / durable-identity / name / none) — operator-only
// diagnostics surfaced in Mission Control, never on the public wire.
func setArtworkPath(r *domain.SearchResult, path string) {
	if r.Extras == nil {
		r.Extras = map[string]any{}
	}
	r.Extras["artwork_path"] = path
}

// artworkPathFor names the resolution path from the chain's outcome. "durable-
// identity" means the identity came from the persisted store (MusicBrainz was
// absent this search) — the deterministic fix firing.
func artworkPathFor(resolved string, confidence ports.ArtworkConfidence, fromDurable bool) string {
	if resolved == "" {
		return "none"
	}
	switch {
	case confidence >= ports.ArtworkConfidenceIdentity && fromDurable:
		return "durable-identity"
	case confidence >= ports.ArtworkConfidenceIdentity:
		return "identity"
	case confidence == ports.ArtworkConfidenceName:
		return "name"
	default:
		return "provider"
	}
}

// resolveArtwork resolves artwork identity-first and reports a confidence so the
// cache can trust the result accordingly. When the entity carries a proven identity
// (bridged provider ids), an identity source (Discogs by id) fetches the exact
// entity's image → ArtworkConfidenceIdentity. Only if no identity source has the
// image does it fall back to a NAME search → ArtworkConfidenceName: provisional,
// short-TTL, overwritable, because for a same-name artist that may be the wrong
// face — it must never masquerade as identity. (It does NOT fall back to a same-
// name *track* cover for an artist.) Nothing resolved → "" / None → honest
// placeholder, never a stranger's face frozen as identity.
func (s *Service) resolveArtwork(ctx context.Context, result domain.SearchResult, mbid string) (string, string, ports.ArtworkConfidence) {
	identity := artworkIdentity(result, mbid)

	// Identity-first: a proven bridged id (Discogs) returns the exact entity's
	// image — the only trustworthy result for a same-name artist.
	if identity.HasLinks() {
		if url, src, _ := s.artworkResolver.ResolveWithIdentityTagged(ctx, result.Kind, result.Title, result.Subtitle, identity); url != "" {
			return url, src, ports.ArtworkConfidenceIdentity
		}
	}
	// No identity image: a NAME search, labeled provisional (short TTL,
	// overwritable). For an ambiguous artist this may be the wrong face, so it
	// must never be labeled identity — a real identity image can replace it later.
	if url, src, _ := s.artworkResolver.ResolveTagged(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
		return url, src, ports.ArtworkConfidenceName
	}
	return "", "", ports.ArtworkConfidenceNone
}

// artworkIdentity assembles the proven cross-provider identity for artwork
// resolution: the MBID plus the bridged provider ids the merge stamped on Xref
// (MB → discogs/spotify/deezer/…).
func artworkIdentity(result domain.SearchResult, mbid string) ports.ArtworkIdentity {
	id := ports.ArtworkIdentity{MBID: mbid}
	if len(result.Xref) > 0 {
		id.ExternalIDs = result.Xref
	}
	return id
}
