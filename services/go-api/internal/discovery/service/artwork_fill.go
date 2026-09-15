package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

// enrichmentNameKey builds the name key used to read and write the shared MBID
// index. It must stay byte-for-byte identical to the MusicBrainz enricher's key
// (service/enrich) so RememberMBID writes and LookupMBID reads line up.
func enrichmentNameKey(title, subtitle string) string {
	return textnorm.NormalizeForMatch(strings.TrimSpace(title) + " " + strings.TrimSpace(subtitle))
}

const (
	artworkFillLimit       = 50
	artworkFillConcurrency = 8
	artworkFillTimeout     = 4 * time.Second
	emptyArtHash           = "d41d8cd98f00b204e9800998ecf8427e"
)

// ArtworkFiller is the artwork-resolution collaborator: it fills missing cover
// art on the top of a ranked slate from the artwork cache, the durable identity
// store, the shared MBID index, and the tagging artwork resolver. It replaces
// the four artwork/identity-lookup ports that used to sit on the Service god
// object, so artwork sourcing can change without touching the orchestrator.
// A nil resolver disables the fill and leaves results untouched.
type ArtworkFiller struct {
	resolver      ports.TaggingArtworkResolver
	cache         ports.ArtworkCache
	identityStore ports.IdentityStore
	mbidIndex     ports.MBIDIndex
}

func newArtworkFiller(
	resolver ports.TaggingArtworkResolver,
	cache ports.ArtworkCache,
	identityStore ports.IdentityStore,
	mbidIndex ports.MBIDIndex,
) *ArtworkFiller {
	return &ArtworkFiller{resolver: resolver, cache: cache, identityStore: identityStore, mbidIndex: mbidIndex}
}

func (a *ArtworkFiller) fill(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if a.resolver == nil {
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
		filled[i] = r
		g.Go(func() error {
			defer RecoverGoroutine(ctx, "artwork_fill.panic", "title", r.Title)
			filled[i] = a.fillOne(fillCtx, r)
			return nil
		})
	}
	_ = g.Wait()
	return append(filled, rest...)
}

func (a *ArtworkFiller) fillOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	needsArt := result.ImageURL == "" || strings.Contains(result.ImageURL, emptyArtHash)
	if !needsArt {
		if result.ArtworkSource == "" && len(result.Sources) > 0 {
			result.ArtworkSource = result.Sources[0].Provider.String()
		}
		setArtworkPath(&result, "provider")
		return result
	}
	mbid, fromDurable := a.lookupIdentity(ctx, &result)

	if a.applyCached(ctx, &result, mbid) {
		return result
	}

	resolved, source, confidence := a.resolve(ctx, result, mbid)
	if a.cache != nil {
		_ = a.cache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved, source, confidence)
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

// lookupIdentity resolves the MBID to key artwork on: the durable identity store
// first (which may also stamp the result's xref), then the shared MBID index.
// fromDurable reports whether the durable store knew the result.
func (a *ArtworkFiller) lookupIdentity(ctx context.Context, result *domain.SearchResult) (mbid string, fromDurable bool) {
	mbid = result.MBID
	if len(result.Xref) == 0 && a.identityStore != nil && len(result.Sources) > 0 {
		src := result.Sources[0]
		if m, xref, ok := a.identityStore.LookupByProviderID(ctx, result.Kind, src.Provider.String(), src.ExternalID); ok {
			fromDurable = true
			if mbid == "" {
				mbid = m
			}
			if len(xref) > 0 {
				result.Xref = xref
			}
			slog.DebugContext(ctx, "identity.durable_resolved",
				"kind", result.Kind.String(), "provider", src.Provider.String(),
				"external_id", src.ExternalID, "mbid", m, "bridged_ids", len(xref))
		}
	}
	if mbid == "" && a.mbidIndex != nil {
		if m, ok := a.mbidIndex.LookupMBID(ctx, result.Kind, enrichmentNameKey(result.Title, result.Subtitle)); ok {
			mbid = m
		}
	}
	return mbid, fromDurable
}

// applyCached consults the artwork cache and reports whether the cached entry
// settled the result (a usable URL, or a cached miss for a non-artist kind), in
// which case the live resolver must not run.
func (a *ArtworkFiller) applyCached(ctx context.Context, result *domain.SearchResult, mbid string) bool {
	if a.cache == nil {
		return false
	}
	cachedURL, cachedSource, found, _ := a.cache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid)
	if !found {
		return false
	}
	if cachedURL != "" && !strings.Contains(cachedURL, emptyArtHash) {
		result.ImageURL = cachedURL
		result.ArtworkSource = cachedSource
		setArtworkPath(result, "cache")
		return true
	}
	if result.Kind != domain.ResultKindArtist {
		setArtworkPath(result, "none")
		return true
	}
	return false
}

func setArtworkPath(r *domain.SearchResult, path string) {
	r.PutExtra("artwork_path", path)
}

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

func (a *ArtworkFiller) resolve(ctx context.Context, result domain.SearchResult, mbid string) (string, string, ports.ArtworkConfidence) {
	identity := artworkIdentity(result, mbid)

	if identity.HasLinks() {
		if url, src, _ := a.resolver.ResolveWithIdentityTagged(ctx, result.Kind, result.Title, result.Subtitle, identity); url != "" {
			return url, src, ports.ArtworkConfidenceIdentity
		}
	}
	if url, src, _ := a.resolver.ResolveTagged(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
		return url, src, ports.ArtworkConfidenceName
	}
	return "", "", ports.ArtworkConfidenceNone
}

func artworkIdentity(result domain.SearchResult, mbid string) ports.ArtworkIdentity {
	id := ports.ArtworkIdentity{MBID: mbid}
	if len(result.Xref) > 0 {
		id.ExternalIDs = result.Xref
	}
	return id
}
