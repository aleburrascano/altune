package artistpage

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/albumgrouping"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"sort"
)

func (s *GetArtistContentService) v2Albums(ctx context.Context, identity ResolvedArtistIdentity, artistRef string) (albums []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	groups, anchorFailed := s.verifyGroupsAgainstMB(ctx, identity, groups)
	merged := albumgrouping.MergeReleases(groups)
	s.discographyTelemetry.emit(ctx, artistRef, merged)
	kept := dropSoundCloudOnly(identity, albumgrouping.FilterCohesive(albumgrouping.FilterKept(merged)))
	out := make([]domain.SearchResult, 0, len(kept))
	for i := range kept {
		r := kept[i].Result
		domain.NormalizeReleaseYear(&r)
		stampRecordType(&r, albumgrouping.NormalizeRecordType(kept[i]))
		out = append(out, r)
	}
	domain.SortByReleaseDateDesc(out, domain.AlbumReleaseSortKey)
	return out, partial || anchorFailed
}

func (s *GetArtistContentService) v2TopTracks(ctx context.Context, identity ResolvedArtistIdentity) (tracks []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistTopTracks(ctx, provider, id)
	})
	groups = s.dropMixedPageTracks(ctx, identity, groups)
	kept := dropSoundCloudOnly(identity, albumgrouping.FilterCohesive(albumgrouping.FilterKept(albumgrouping.MergeReleases(groups))))
	sort.SliceStable(kept, func(i, j int) bool {
		return len(kept[i].Providers) > len(kept[j].Providers)
	})
	out := make([]domain.SearchResult, 0, len(kept))
	for _, m := range kept {
		out = append(out, m.Result)
	}
	return out, partial
}

func (s *GetArtistContentService) dropMixedPageTracks(ctx context.Context, identity ResolvedArtistIdentity, trackGroups []albumgrouping.ReleaseGroup) []albumgrouping.ReleaseGroup {
	if s.mbAnchor == nil || identity.MBID == "" {
		return trackGroups
	}
	albumGroups, _ := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	titles, err := providerguard.GuardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]string, error) {
		return s.mbAnchor.ReleaseGroupTitles(ctx, identity.MBID)
	})
	if err != nil || len(titles) == 0 {
		return trackGroups
	}
	return albumgrouping.FilterMixedPageTracks(albumgrouping.NormalizeTitleSet(titles), albumGroups, trackGroups)
}

func (s *GetArtistContentService) v2ReleaseGroups(ctx context.Context, identity ResolvedArtistIdentity, fetch identityContentFetch) (releaseGroups []albumgrouping.ReleaseGroup, partial bool) {
	idGroups, partial := s.fanOutByIdentity(ctx, identity, fetch)
	groups := make([]albumgrouping.ReleaseGroup, 0, len(idGroups))
	for _, g := range idGroups {
		groups = append(groups, albumgrouping.ReleaseGroup{Releases: g, IDVerified: true})
	}
	return groups, partial
}

func (s *GetArtistContentService) verifyGroupsAgainstMB(ctx context.Context, identity ResolvedArtistIdentity, groups []albumgrouping.ReleaseGroup) (out []albumgrouping.ReleaseGroup, anchorFailed bool) {
	if s.mbAnchor == nil || identity.MBID == "" {
		return groups, false
	}
	titles, err := providerguard.GuardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]string, error) {
		return s.mbAnchor.ReleaseGroupTitles(ctx, identity.MBID)
	})
	if err != nil {
		if ctx.Err() != nil {
			return groups, false
		}
		slog.WarnContext(ctx, "artist_albums.mb_anchor_failed", "mbid", identity.MBID, "error", redact.Secrets(err.Error()))
		return groups, true
	}
	if len(titles) == 0 {
		return groups, false
	}
	return albumgrouping.FilterGroupsByMBAnchor(albumgrouping.NormalizeTitleSet(titles), groups), false
}

func dropSoundCloudOnly(identity ResolvedArtistIdentity, releases []albumgrouping.MergedRelease) []albumgrouping.MergedRelease {
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
