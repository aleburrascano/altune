package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"time"
)

const (
	disambigTimeout    = 2 * time.Second
	disambigMaxLookups = 3
)

type artistDisambiguator struct {
	validator ports.ArtistIdentityResolver
}

func newArtistDisambiguator(validator ports.ArtistIdentityResolver) *artistDisambiguator {
	return &artistDisambiguator{validator: validator}
}

func (s *artistDisambiguator) apply(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	applyDisambiguationExtras(results)
	if s.validator == nil {
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
			identity, err := s.validator.ResolveArtistIdentity(ctx, r.Title)
			entry = cached{identity: identity, ok: err == nil && identity != nil}
			identityCache[nameNorm] = entry
		}
		if !entry.ok {
			continue
		}
		applyArtistIdentity(&results[i], entry.identity)
	}
	return results
}

func applyDisambiguationExtras(results []domain.SearchResult) {
	for i, r := range results {
		if r.Kind != domain.ResultKindArtist || r.Subtitle != "" {
			continue
		}
		if disambig, _ := r.Extras[domain.ExtraDisambiguation].(string); disambig != "" {
			results[i].Subtitle = disambig
		}
	}
}

func applyArtistIdentity(r *domain.SearchResult, identity *ports.ArtistIdentity) {
	if identity.Disambiguation != "" {
		r.Subtitle = identity.Disambiguation
		*r = r.WithExtra(domain.ExtraDisambiguation, identity.Disambiguation)
	}
	if identity.MBID != "" && r.MBID == "" {
		r.MBID = identity.MBID
	}
}
