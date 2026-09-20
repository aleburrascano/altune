package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"log/slog"
	"net/url"
)

func (a *MusicBrainzAdapter) ValidateArtistAlbums(
	ctx context.Context,
	artistName string,
	albums []domain.SearchResult,
) (*ports.AlbumValidationResult, error) {
	mbid, err := a.resolveArtistMBID(ctx, artistName)
	if err != nil {
		slog.WarnContext(ctx, "mb.resolve_mbid_failed", "artist", artistName, "error", err)
		return nil, fmt.Errorf("mb resolve failed: %w", err)
	}
	if mbid == "" {
		slog.InfoContext(ctx, "mb.no_mbid_found", "artist", artistName)
		return nil, fmt.Errorf("mb artist not found for %q", artistName)
	}
	slog.InfoContext(ctx, "mb.artist_resolved", "artist", artistName, "mbid", mbid)

	releases, err := a.fetchReleaseGroups(ctx, mbid)
	if err != nil {
		slog.WarnContext(ctx, "mb.release_groups_failed", "mbid", mbid, "error", err)
		return nil, fmt.Errorf("mb release-groups unavailable: %w", err)
	}

	mbTitles := make(map[string]bool, len(releases))
	for _, rg := range releases {
		mbTitles[textnorm.NormalizeForMatch(rg.Title)] = true
	}

	var confirmed, unconfirmed []domain.SearchResult
	for _, album := range albums {
		if mbTitles[textnorm.NormalizeForMatch(album.Title)] {
			confirmed = append(confirmed, album)
		} else {
			unconfirmed = append(unconfirmed, album)
		}
	}

	slog.InfoContext(ctx, "mb.album_validation",
		"artist", artistName, "mbid", mbid,
		"mb_releases", len(releases),
		"confirmed", len(confirmed),
		"unconfirmed", len(unconfirmed),
	)

	return &ports.AlbumValidationResult{
		Confirmed:   confirmed,
		Unconfirmed: unconfirmed,
		ArtistMBID:  mbid,
	}, nil
}

func (a *MusicBrainzAdapter) ListArtistDiscography(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
	mbid, err := a.resolveArtistMBID(ctx, artistName)
	if err != nil {
		return nil, err
	}
	if mbid == "" {
		return nil, nil
	}
	rgs, err := a.fetchReleaseGroups(ctx, mbid)
	if err != nil {
		return nil, err
	}
	results := make([]domain.SearchResult, 0, len(rgs))
	for _, rg := range rgs {
		results = append(results, mapMBReleaseGroup(rg))
	}
	return results, nil
}

func (a *MusicBrainzAdapter) fetchReleaseGroupMatches(ctx context.Context, query string) ([]mbReleaseGroup, error) {
	u := fmt.Sprintf("https://musicbrainz.org/ws/2/release-group/?query=%s&fmt=json&limit=10",
		url.QueryEscape(query))
	var body mbReleaseGroupResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	return body.ReleaseGroups, nil
}

func (a *MusicBrainzAdapter) ReleaseGroupTitles(ctx context.Context, mbid string) ([]string, error) {
	if mbid == "" {
		return nil, nil
	}
	rgs, err := a.fetchReleaseGroups(ctx, mbid)
	if err != nil {
		return nil, err
	}
	titles := make([]string, 0, len(rgs))
	for _, rg := range rgs {
		titles = append(titles, rg.Title)
	}
	return titles, nil
}

const mbMaxReleaseGroupPages = 5

func (a *MusicBrainzAdapter) fetchReleaseGroups(ctx context.Context, mbid string) ([]mbReleaseGroup, error) {
	if rgs, ok := a.releaseMemo.get(mbid); ok {
		return rgs, nil
	}
	v, err, _ := a.releaseSF.Do(mbid, func() (any, error) {
		if rgs, ok := a.releaseMemo.get(mbid); ok {
			return rgs, nil
		}
		return a.fetchReleaseGroupPages(ctx, mbid)
	})
	if err != nil {
		return nil, err
	}
	return v.([]mbReleaseGroup), nil
}

func (a *MusicBrainzAdapter) fetchReleaseGroupPages(ctx context.Context, mbid string) ([]mbReleaseGroup, error) {
	fetched := 0
	degraded := false
	all, err := fetchPaged(mbMaxReleaseGroupPages,
		func(page int) ([]mbReleaseGroup, bool, error) {
			u := fmt.Sprintf(
				"https://musicbrainz.org/ws/2/release-group?artist=%s&type=album%%7Cep%%7Csingle&fmt=json&limit=100&offset=%d",
				url.QueryEscape(mbid), page*100)

			var body mbReleaseGroupResponse
			if err := a.getJSON(ctx, u, &body); err != nil {
				return nil, false, err
			}
			fetched += len(body.ReleaseGroups)
			more := len(body.ReleaseGroups) != 0 && fetched < body.ReleaseGroupCount
			return body.ReleaseGroups, more, nil
		},
		func(page int, err error) {
			degraded = true
			slog.DebugContext(ctx, "mb.release_groups_page_failed",
				"mbid", mbid, "page", page, "error", err)
		})
	if err != nil {
		return nil, err
	}
	// Memoize only a complete discography; a partial set from the later-page
	// degrade path must not be cached and reused.
	if !degraded {
		a.releaseMemo.put(mbid, all)
	}
	return all, nil
}

func extractCreditedMBID(rg mbReleaseGroup) string {
	if len(rg.ArtistCredit) == 0 {
		return ""
	}
	if rg.ArtistCredit[0].Artist == nil {
		return ""
	}
	return rg.ArtistCredit[0].Artist.ID
}
