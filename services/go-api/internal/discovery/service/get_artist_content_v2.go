package service

import (
	"context"
	"sort"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func (s *GetArtistContentService) v2Albums(ctx context.Context, identity ResolvedArtistIdentity, artistName string) []domain.SearchResult {
	groups := s.v2ReleaseGroups(ctx, identity, artistName, false, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	groups = s.verifyGroupsAgainstMB(ctx, identity, groups)
	kept := FilterCohesive(FilterKept(MergeReleases(groups)))
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

func (s *GetArtistContentService) v2TopTracks(ctx context.Context, identity ResolvedArtistIdentity, artistName string) []domain.SearchResult {
	groups := s.v2ReleaseGroups(ctx, identity, artistName, false, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistTopTracks(ctx, provider, id)
	})
	kept := FilterCohesive(FilterKept(MergeReleases(groups)))
	sort.SliceStable(kept, func(i, j int) bool {
		return len(kept[i].Providers) > len(kept[j].Providers)
	})
	out := make([]domain.SearchResult, 0, len(kept))
	for _, m := range kept {
		out = append(out, m.Result)
	}
	return out
}

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

func (s *GetArtistContentService) verifyGroupsAgainstMB(ctx context.Context, identity ResolvedArtistIdentity, groups []ReleaseGroup) []ReleaseGroup {
	if s.mbAnchor == nil || identity.MBID == "" {
		return groups
	}
	titles, err := s.mbAnchor.ReleaseGroupTitles(ctx, identity.MBID)
	if err != nil || len(titles) == 0 {
		return groups
	}
	return FilterGroupsByMBAnchor(normalizeTitleSet(titles), groups)
}

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
