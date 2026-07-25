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
	artworkFillLimit       = 50
	artworkFillConcurrency = 8
	artworkFillTimeout     = 4 * time.Second
	emptyArtHash           = "d41d8cd98f00b204e9800998ecf8427e"
)

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

func (s *Service) fillArtworkOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	needsArt := result.ImageURL == "" || strings.Contains(result.ImageURL, emptyArtHash)
	if !needsArt {
		if result.ArtworkSource == "" && len(result.Sources) > 0 {
			result.ArtworkSource = result.Sources[0].Provider.String()
		}
		setArtworkPath(&result, "provider")
		return result
	}
	mbid := result.MBID
	fromDurable := false
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
			slog.DebugContext(ctx, "identity.durable_resolved",
				"kind", result.Kind.String(), "provider", src.Provider.String(),
				"external_id", src.ExternalID, "mbid", m, "bridged_ids", len(xref))
		}
	}
	if mbid == "" && s.mbidIndex != nil {
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

func setArtworkPath(r *domain.SearchResult, path string) {
	if r.Extras == nil {
		r.Extras = map[string]any{}
	}
	r.Extras["artwork_path"] = path
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

func (s *Service) resolveArtwork(ctx context.Context, result domain.SearchResult, mbid string) (string, string, ports.ArtworkConfidence) {
	identity := artworkIdentity(result, mbid)

	if identity.HasLinks() {
		if url, src, _ := s.artworkResolver.ResolveWithIdentityTagged(ctx, result.Kind, result.Title, result.Subtitle, identity); url != "" {
			return url, src, ports.ArtworkConfidenceIdentity
		}
	}
	if url, src, _ := s.artworkResolver.ResolveTagged(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
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
