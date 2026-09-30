package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"
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
	var partial *domain.PartialResultError
	if err != nil && !errors.As(err, &partial) {
		return nil, err
	}
	results := make([]domain.SearchResult, 0, len(rgs))
	for _, rg := range rgs {
		results = append(results, mapMBReleaseGroup(rg))
	}
	return results, err
}

func (a *MusicBrainzAdapter) fetchReleaseGroupMatches(ctx context.Context, query string) ([]mbReleaseGroup, error) {
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/release-group/?query=%s&fmt=json&limit=10",
		url.QueryEscape(mbLuceneEscape(query)))
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

const mbReleaseGroupsFlightTimeout = 60 * time.Second

func (a *MusicBrainzAdapter) fetchReleaseGroups(ctx context.Context, mbid string) ([]mbReleaseGroup, error) {
	if rgs, ok := a.releaseMemo.get(mbid); ok {
		return rgs, nil
	}
	ch := a.releaseSF.DoChan(mbid, func() (any, error) {
		if rgs, ok := a.releaseMemo.get(mbid); ok {
			return rgs, nil
		}
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mbReleaseGroupsFlightTimeout)
		defer cancel()
		return a.fetchReleaseGroupPages(fctx, mbid)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		rgs, _ := res.Val.([]mbReleaseGroup)
		return rgs, res.Err
	}
}

func (a *MusicBrainzAdapter) fetchReleaseGroupPages(ctx context.Context, mbid string) ([]mbReleaseGroup, error) {
	fetched := 0
	all, err := fetchPaged(mbMaxReleaseGroupPages,
		func(page int) ([]mbReleaseGroup, bool, error) {
			u := fmt.Sprintf(
				musicbrainzAPIBaseURL+"/release-group?artist=%s&type=album%%7Cep%%7Csingle&fmt=json&limit=100&offset=%d",
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
			slog.WarnContext(ctx, "mb.release_groups_page_failed",
				"provider", domain.ProviderMusicBrainz.String(), "mbid", mbid, "page", page, "error", redact.Secrets(err.Error()))
		})
	if err != nil {
		return all, err
	}
	a.releaseMemo.put(mbid, all)
	return all, nil
}
