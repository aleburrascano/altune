package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"sort"
)

func (s *GetArtistContentService) v2Albums(ctx context.Context, identity ResolvedArtistIdentity, artistRef string) (albums []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	groups = s.verifyGroupsAgainstMB(ctx, identity, groups)
	merged := MergeReleases(groups)
	// The merge is where cross-provider disagreement is already computed;
	// record the structural-quality signal best-effort, off the response path.
	s.discographyTelemetry.emit(ctx, artistRef, merged)
	kept := FilterCohesive(FilterKept(merged))
	out := make([]domain.SearchResult, 0, len(kept))
	for i := range kept {
		r := kept[i].Result
		normalizeReleaseYear(&r)
		stampRecordType(&r, NormalizeRecordType(kept[i]))
		out = append(out, r)
	}
	sortByReleaseDateDesc(out, albumReleaseSortKey)
	return out, partial
}

func (s *GetArtistContentService) v2TopTracks(ctx context.Context, identity ResolvedArtistIdentity) (tracks []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
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
	return out, partial
}

func (s *GetArtistContentService) v2ReleaseGroups(ctx context.Context, identity ResolvedArtistIdentity, fetch identityContentFetch) (releaseGroups []ReleaseGroup, partial bool) {
	idGroups, partial := s.fanOutByIdentity(ctx, identity, "", fetch)
	groups := make([]ReleaseGroup, 0, len(idGroups))
	for _, g := range idGroups {
		groups = append(groups, ReleaseGroup{Releases: g, IDVerified: true})
	}
	return groups, partial
}

func (s *GetArtistContentService) verifyGroupsAgainstMB(ctx context.Context, identity ResolvedArtistIdentity, groups []ReleaseGroup) []ReleaseGroup {
	if s.mbAnchor == nil || identity.MBID == "" {
		return groups
	}
	titles, err := guardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]string, error) {
		return s.mbAnchor.ReleaseGroupTitles(ctx, identity.MBID)
	})
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

func stampRecordType(r *domain.SearchResult, recordType domain.RecordType) {
	r.RecordType = recordType
}
