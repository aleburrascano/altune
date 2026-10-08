package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
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
	merged := MergeReleases(groups)
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
	return out, partial || anchorFailed
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
	idGroups, partial := s.fanOutByIdentity(ctx, identity, fetch)
	groups := make([]ReleaseGroup, 0, len(idGroups))
	for _, g := range idGroups {
		groups = append(groups, ReleaseGroup{Releases: g, IDVerified: true})
	}
	return groups, partial
}

func (s *GetArtistContentService) verifyGroupsAgainstMB(ctx context.Context, identity ResolvedArtistIdentity, groups []ReleaseGroup) (out []ReleaseGroup, anchorFailed bool) {
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
	return FilterGroupsByMBAnchor(normalizeTitleSet(titles), groups), false
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
