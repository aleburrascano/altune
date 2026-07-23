package service

import (
	"context"
	"sort"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// Discography V2 (docs/discovery-detail-pipeline.md §6) — the rebuilt core wired
// into the artist-content service behind DISCOGRAPHY_V2. It replaces the lossy
// Merge → consensus(MB-veto) → hideBare path with the pure cores:
//
//	id-verified fan-out + by-name completeness groups
//	  → MergeReleases (field-level best-of, never replace)
//	  → FilterKept   (keep on corroboration/identifier/own-id, not MB-veto)
//	  → normalize record_type + year, order
//
// Contamination-safe (no blind SoundCloud name-resolve: the id fan-out is queried
// by id only), and metadata-complete (every field best-of'd), so the year/track-
// count/bucketing bugs are fixed at the source.

// v2Albums builds the discography identity-first with the V2 cores.
func (s *GetArtistContentService) v2Albums(ctx context.Context, identity ResolvedArtistIdentity, artistName string) []domain.SearchResult {
	groups := s.v2ReleaseGroups(ctx, identity, artistName, true, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	kept := FilterKept(MergeReleases(groups))
	out := make([]domain.SearchResult, 0, len(kept))
	for i := range kept {
		r := kept[i].Result
		normalizeReleaseYear(&r)
		stampRecordType(&r, NormalizeRecordType(kept[i]))
		out = append(out, r)
	}
	sortAlbumsByReleaseDateDesc(out)
	return out
}

// v2TopTracks builds top tracks with the V2 cores. There is no by-name
// completeness feed for tracks (consensus is album-only), so only the id-verified
// fan-out contributes; results order most-corroborated first.
func (s *GetArtistContentService) v2TopTracks(ctx context.Context, identity ResolvedArtistIdentity, artistName string) []domain.SearchResult {
	groups := s.v2ReleaseGroups(ctx, identity, artistName, false, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistTopTracks(ctx, provider, id)
	})
	kept := FilterKept(MergeReleases(groups))
	sort.SliceStable(kept, func(i, j int) bool {
		return len(kept[i].Providers) > len(kept[j].Providers)
	})
	out := make([]domain.SearchResult, 0, len(kept))
	for _, m := range kept {
		out = append(out, m.Result)
	}
	return out
}

// v2ReleaseGroups assembles the tagged input for MergeReleases: the id fan-out
// (queried by each provider's OWN id — IDVerified, never a name, so no same-name
// bleed) plus, for albums, the by-name consensus providers as unverified groups
// the keep step filters. artistName is intentionally NOT passed to fanOutByIdentity
// (that would re-enable the blind SoundCloud name-resolve V2 removes).
func (s *GetArtistContentService) v2ReleaseGroups(ctx context.Context, identity ResolvedArtistIdentity, artistName string, includeNameGroups bool, fetch identityContentFetch) []ReleaseGroup {
	idGroups := s.fanOutByIdentity(ctx, identity, "", fetch)
	groups := make([]ReleaseGroup, 0, len(idGroups)+8)
	for _, g := range idGroups {
		groups = append(groups, ReleaseGroup{Releases: g, IDVerified: true})
	}
	if includeNameGroups && s.consensus != nil && artistName != "" {
		for _, g := range s.consensus.NameGroups(ctx, artistName) {
			groups = append(groups, ReleaseGroup{Releases: g, IDVerified: false})
		}
	}
	return groups
}

// normalizeReleaseYear derives a numeric Year from ReleaseDate when a provider
// carried only the date (mirrors normalizeAlbumYears for a single record).
func normalizeReleaseYear(r *domain.SearchResult) {
	if r.Year != 0 || len(r.ReleaseDate) < 4 {
		return
	}
	if y := parseYear(r.ReleaseDate[:4]); y > 0 {
		r.Year = y
	}
}

func stampRecordType(r *domain.SearchResult, recordType string) {
	if r.Extras == nil {
		r.Extras = map[string]any{}
	}
	r.Extras["record_type"] = recordType
}
