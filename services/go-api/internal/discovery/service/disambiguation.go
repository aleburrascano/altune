package service

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

const (
	// disambigTimeout bounds the whole live-disambiguation pass. Like enrich and
	// FindRelated, this stage must not dominate the search hot path.
	disambigTimeout = 2 * time.Second
	// disambigMaxLookups caps live MusicBrainz identity resolutions per search.
	// MB is rate-limited to ~1 req/s, so each lookup costs seconds and they can't
	// be parallelized away. Results are ranked, so the top few artists (the ones
	// actually shown) get disambiguated while lower-ranked same-name artists keep
	// an empty subtitle. Pre-resolved disambiguations carried in extras are free
	// and always applied regardless of this cap.
	disambigMaxLookups = 3
)

// applyArtistDisambiguation fills an artist result's empty subtitle with a
// disambiguation hint ("American rapper", "English rock band") so same-name
// artists are distinguishable. It prefers a disambiguation already carried in
// extras and otherwise resolves identity via MusicBrainz. Live MB resolution is
// bounded by disambigTimeout and disambigMaxLookups so it can never stall search
// (it was previously unbounded — one sequential rate-limited MB call per distinct
// artist name, ~15s on multi-artist queries).
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
		if disambig := stringExtra(r, "disambiguation"); disambig != "" {
			results[i].Subtitle = disambig
			continue
		}

		nameNorm := textnorm.NormalizeForMatch(r.Title)
		entry, found := identityCache[nameNorm]
		if !found {
			// Bound live MB resolution: stop issuing new lookups once the
			// per-search budget is spent or the timeout has fired. Names already
			// resolved this request still apply for free.
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
