package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
	"sort"
)

func (s *GetArtistContentService) v2Albums(ctx context.Context, identity ResolvedArtistIdentity, artistRef string) (albums []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	groups, anchorFailed := s.anchoring.verifyGroups(ctx, identity, groups)
	merged := domain.MergeReleases(groups)
	s.discography.emit(ctx, artistRef, merged)
	kept := dropSoundCloudOnly(identity, domain.FilterCohesive(domain.FilterKept(merged)))
	out := make([]domain.SearchResult, 0, len(kept))
	for i := range kept {
		r := kept[i].Result
		domain.NormalizeReleaseYear(&r)
		stampRecordType(&r, domain.NormalizeRecordType(kept[i]))
		out = append(out, r)
	}
	domain.SortByReleaseDateDesc(out, domain.AlbumReleaseSortKey)
	return out, partial || anchorFailed
}

func (s *GetArtistContentService) v2TopTracks(ctx context.Context, identity ResolvedArtistIdentity) (tracks []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistTopTracks(ctx, provider, id)
	})
	groups = s.anchoring.dropMixedPageTracks(ctx, identity, groups, s.albumReleaseGroups)
	kept := dropSoundCloudOnly(identity, domain.FilterCohesive(domain.FilterKept(domain.MergeReleases(groups))))
	sort.SliceStable(kept, func(i, j int) bool {
		return len(kept[i].Providers) > len(kept[j].Providers)
	})
	out := make([]domain.SearchResult, 0, len(kept))
	for _, m := range kept {
		out = append(out, m.Result)
	}
	return out, partial
}

func (s *GetArtistContentService) albumReleaseGroups(ctx context.Context, identity ResolvedArtistIdentity) []domain.ReleaseGroup {
	groups, _ := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	return groups
}

func (s *GetArtistContentService) v2ReleaseGroups(ctx context.Context, identity ResolvedArtistIdentity, fetch identityContentFetch) (releaseGroups []domain.ReleaseGroup, partial bool) {
	idGroups, partial := s.fanOutByIdentity(ctx, identity, fetch)
	groups := make([]domain.ReleaseGroup, 0, len(idGroups))
	for _, g := range idGroups {
		groups = append(groups, domain.ReleaseGroup{Releases: g, IDVerified: true})
	}
	return groups, partial
}

func dropSoundCloudOnly(identity ResolvedArtistIdentity, releases []domain.MergedRelease) []domain.MergedRelease {
	if !identity.knownToACatalog() {
		return releases
	}
	kept := releases[:0]
	for _, r := range releases {
		if len(r.Providers) != 1 || !r.Providers[domain.ProviderSoundCloud] {
			kept = append(kept, r)
		}
	}
	return kept
}

func stampRecordType(r *domain.SearchResult, recordType domain.RecordType) {
	r.RecordType = recordType
}
