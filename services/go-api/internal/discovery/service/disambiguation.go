package service

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

const (
	disambigTimeout    = 2 * time.Second
	disambigMaxLookups = 3
)

func (s *Service) applyArtistDisambiguation(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	for i, r := range results {
		if r.Kind != domain.ResultKindArtist || r.Subtitle != "" {
			continue
		}
		if disambig, _ := r.Extras["disambiguation"].(string); disambig != "" {
			results[i].Subtitle = disambig
		}
	}
	if s.albumValidator == nil {
		return results
	}

	ctx, cancel := context.WithTimeout(ctx, disambigTimeout)
	defer cancel()

	type cached struct {
		identity *ports.ArtistIdentity
		ok       bool
	}
	identityCache := make(map[string]cached)
	liveLookups := 0

	for i, r := range results {
		if r.Kind != domain.ResultKindArtist || r.Subtitle != "" {
			continue
		}

		nameNorm := textnorm.NormalizeForMatch(r.Title)
		entry, found := identityCache[nameNorm]
		if !found {
			if liveLookups >= disambigMaxLookups || ctx.Err() != nil {
				continue
			}
			liveLookups++
			identity, err := s.albumValidator.ResolveArtistIdentity(ctx, r.Title)
			entry = cached{identity: identity, ok: err == nil && identity != nil}
			identityCache[nameNorm] = entry
		}
		if !entry.ok {
			continue
		}

		extras := copyExtras(r.Extras)
		if entry.identity.Disambiguation != "" {
			results[i].Subtitle = entry.identity.Disambiguation
			extras["disambiguation"] = entry.identity.Disambiguation
		}
		if entry.identity.MBID != "" && results[i].MBID == "" {
			results[i].MBID = entry.identity.MBID
		}
		results[i].Extras = extras
	}
	return results
}
