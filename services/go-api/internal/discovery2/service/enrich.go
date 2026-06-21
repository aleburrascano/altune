package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
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

// enrichOne fills a single result's artwork: a usable cache hit short-circuits;
// otherwise resolve via the chain and memoize. Artists always try (to upgrade a
// channel thumbnail) and fall back to a same-name track image.
func (s *Service) enrichOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	needsArt := result.ImageURL == "" || strings.Contains(result.ImageURL, emptyArtHash)
	tryArt := needsArt || result.Kind == domain.ResultKindArtist
	if !tryArt {
		return result
	}
	mbid := stringExtra(result, "mbid")

	if s.artworkCache != nil {
		if cachedURL, found, _ := s.artworkCache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid); found {
			usable := cachedURL != "" && !strings.Contains(cachedURL, emptyArtHash)
			if usable {
				result.ImageURL = cachedURL
				return result
			}
			// Cached miss/placeholder: only artists retry (track-image fallback).
			if result.Kind != domain.ResultKindArtist {
				return result
			}
		}
	}

	resolved := s.resolveArtwork(ctx, result, mbid)
	if s.artworkCache != nil {
		_ = s.artworkCache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved)
	}
	if resolved != "" {
		result.ImageURL = resolved
	}
	return result
}

// resolveArtwork resolves via the chained resolver, with a same-name track
// fallback for artists whose direct lookup returns nothing.
func (s *Service) resolveArtwork(ctx context.Context, result domain.SearchResult, mbid string) string {
	if url, _ := s.artworkResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
		return url
	}
	if result.Kind == domain.ResultKindArtist {
		if url, _ := s.artworkResolver.Resolve(ctx, domain.ResultKindTrack, result.Title, "", ""); url != "" {
			return url
		}
	}
	return ""
}
