package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// applyArtistDisambiguation fills an artist result's empty subtitle with a
// disambiguation hint ("American rapper", "English rock band") so same-name
// artists are distinguishable. It prefers a disambiguation already carried in
// extras and otherwise resolves identity via MusicBrainz (cached per name).
func (s *Service) applyArtistDisambiguation(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if s.albumValidator == nil {
		for i, r := range results {
			if r.Kind != domain.ResultKindArtist || r.Subtitle != "" {
				continue
			}
			if disambig := stringExtra(r, "disambiguation"); disambig != "" {
				results[i].Subtitle = disambig
			}
		}
		return results
	}

	type cached struct {
		identity *ports.ArtistIdentity
		ok       bool
	}
	identityCache := make(map[string]cached)

	for i, r := range results {
		if r.Kind != domain.ResultKindArtist || r.Subtitle != "" {
			continue
		}
		if disambig := stringExtra(r, "disambiguation"); disambig != "" {
			results[i].Subtitle = disambig
			continue
		}

		nameNorm := textnorm.NormalizeForMatch(r.Title)
		entry, found := identityCache[nameNorm]
		if !found {
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
		if entry.identity.MBID != "" {
			extras["mbid"] = entry.identity.MBID
		}
		results[i].Extras = extras
	}
	return results
}
