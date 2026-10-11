package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/release"
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
	merged := release.MergeReleases(groups)
	s.discographyTelemetry.emit(ctx, artistRef, merged)
	kept := dropSoundCloudOnly(identity, release.FilterCohesive(release.FilterKept(merged)))
	out := make([]domain.SearchResult, 0, len(kept))
	for i := range kept {
		r := kept[i].Result
		normalizeReleaseYear(&r)
		stampRecordType(&r, release.NormalizeRecordType(kept[i]))
		out = append(out, r)
	}
	sortByReleaseDateDesc(out, albumReleaseSortKey)
	return out, partial || anchorFailed
}

func (s *GetArtistContentService) v2TopTracks(ctx context.Context, identity ResolvedArtistIdentity) (tracks []domain.SearchResult, partial bool) {
	groups, partial := s.v2ReleaseGroups(ctx, identity, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistTopTracks(ctx, provider, id)
	})
	kept := dropSoundCloudOnly(identity, release.FilterCohesive(release.FilterKept(release.MergeReleases(groups))))
	sort.SliceStable(kept, func(i, j int) bool {
		return len(kept[i].Providers) > len(kept[j].Providers)
	})
	out := make([]domain.SearchResult, 0, len(kept))
	for _, m := range kept {
		out = append(out, m.Result)
	}
	return out, partial
}

func (s *GetArtistContentService) v2ReleaseGroups(ctx context.Context, identity ResolvedArtistIdentity, fetch identityContentFetch) (releaseGroups []release.ReleaseGroup, partial bool) {
	idGroups, partial := s.fanOutByIdentity(ctx, identity, fetch)
	groups := make([]release.ReleaseGroup, 0, len(idGroups))
	for _, g := range idGroups {
		groups = append(groups, release.ReleaseGroup{Releases: g, IDVerified: true})
	}
	return groups, partial
}

func (s *GetArtistContentService) verifyGroupsAgainstMB(ctx context.Context, identity ResolvedArtistIdentity, groups []release.ReleaseGroup) (out []release.ReleaseGroup, anchorFailed bool) {
	if s.mbAnchor == nil || identity.MBID == "" {
		return groups, false
	}
	titles, err := guardedFetch(ctx, s.breaker, domain.ProviderMusicBrainz, func() ([]string, error) {
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
	return release.FilterGroupsByMBAnchor(release.NormalizeTitleSet(titles), groups), false
}

func dropSoundCloudOnly(identity ResolvedArtistIdentity, releases []release.MergedRelease) []release.MergedRelease {
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
