package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
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
	var all []mbReleaseGroup
	for page := 0; page < mbMaxReleaseGroupPages; page++ {
		u := fmt.Sprintf(
			"https://musicbrainz.org/ws/2/release-group?artist=%s&type=album%%7Cep%%7Csingle&fmt=json&limit=100&offset=%d",
			url.QueryEscape(mbid), page*100)

		var body mbReleaseGroupResponse
		if err := a.getJSON(ctx, u, &body); err != nil {
			if page > 0 {
				slog.DebugContext(ctx, "mb.release_groups_page_failed",
					"mbid", mbid, "page", page, "error", err)
				return all, nil
			}
			return nil, err
		}
		all = append(all, body.ReleaseGroups...)
		if len(body.ReleaseGroups) == 0 || len(all) >= body.ReleaseGroupCount {
			break
		}
	}
	a.releaseMemo.put(mbid, all)
	return all, nil
}

func (a *MusicBrainzAdapter) LookupAlbumArtist(
	ctx context.Context,
	artistName, albumTitle string,
	profile domain.ArtistIdentityProfile,
) (domain.AlbumVerdict, string, error) {
	q := fmt.Sprintf(`release-group:"%s" AND artist:"%s"`, mbEscapeQuotes(albumTitle), mbEscapeQuotes(artistName))
	u := fmt.Sprintf(
		"https://musicbrainz.org/ws/2/release-group/?query=%s&fmt=json&limit=5",
		url.QueryEscape(q),
	)

	var body mbReleaseGroupResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		slog.DebugContext(ctx, "mb.lookup_album_artist_http_error",
			"artist", artistName, "album", albumTitle, "error", err)
		return domain.AlbumVerdictUnknown, "", nil
	}

	titleNorm := textnorm.NormalizeForMatch(albumTitle)
	for _, rg := range body.ReleaseGroups {
		if textnorm.NormalizeForMatch(rg.Title) != titleNorm {
			continue
		}
		creditedMBID := extractCreditedMBID(rg)
		if creditedMBID == "" {
			continue
		}
		if profile.MBID != "" {
			if creditedMBID == profile.MBID {
				slog.DebugContext(ctx, "mb.lookup_album_artist_confirmed",
					"artist", artistName, "album", albumTitle, "mbid", creditedMBID)
				return domain.AlbumVerdictConfirmed, creditedMBID, nil
			}
			slog.DebugContext(ctx, "mb.lookup_album_artist_contamination",
				"artist", artistName, "album", albumTitle,
				"expected_mbid", profile.MBID, "credited_mbid", creditedMBID)
			return domain.AlbumVerdictContamination, creditedMBID, nil
		}
		slog.DebugContext(ctx, "mb.lookup_album_artist_no_profile_mbid",
			"artist", artistName, "album", albumTitle, "credited_mbid", creditedMBID)
		return domain.AlbumVerdictUnknown, creditedMBID, nil
	}

	slog.DebugContext(ctx, "mb.lookup_album_artist_no_match",
		"artist", artistName, "album", albumTitle)
	return domain.AlbumVerdictUnknown, "", nil
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
