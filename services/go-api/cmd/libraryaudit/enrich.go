package main

import (
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"strconv"
	"strings"
)

type albumCatalog interface {
	AlbumGenres(ctx context.Context, albumID int64) ([]string, error)
}

type releaseCatalog interface {
	RecordingsWithReleases(ctx context.Context, isrc string) ([]recording, error)
}

func sharesLibraryAlbum(lib libraryTrack, cat catalogTrack) bool {
	return strings.TrimSpace(lib.Album) != "" && textnorm.NormalizeForIdentity(lib.Album) == textnorm.NormalizeForIdentity(cat.Album)
}

func enrichmentFindings(ctx context.Context, cat catalog, lib libraryTrack, found catalogTrack) ([]finding, error) {
	var out []finding
	out = append(out, catalogDurationFinding(lib, found)...)
	out = append(out, trackNumberFinding(lib, found)...)
	genre, err := genreFinding(ctx, cat, lib, found)
	if err != nil {
		return nil, err
	}
	return append(out, genre...), nil
}

func catalogDurationFinding(lib libraryTrack, found catalogTrack) []finding {
	if lib.CatalogDurationSeconds > 0 || found.DurationSeconds <= 0 {
		return nil
	}
	seconds := strconv.FormatFloat(found.DurationSeconds, 'f', -1, 64)
	filled := about(lib, categoryCatalogDurationFillable, seconds)
	filled.Fix = fixing(lib, "catalog_duration_seconds", "", seconds)
	return []finding{filled}
}

func trackNumberFinding(lib libraryTrack, found catalogTrack) []finding {
	if lib.TrackNumber > 0 || found.TrackPosition <= 0 {
		return nil
	}
	switch {
	case !sharesLibraryAlbum(lib, found):
		return []finding{about(lib, categoryTrackNumberUnfillable, fmt.Sprintf("position %d is on catalog album %q, library album is %q", found.TrackPosition, found.Album, lib.Album))}
	case found.DiskNumber > 1:
		return []finding{about(lib, categoryTrackNumberUnfillable, fmt.Sprintf("position %d is on disc %d of the catalog album", found.TrackPosition, found.DiskNumber))}
	}
	position := strconv.Itoa(found.TrackPosition)
	filled := about(lib, categoryTrackNumberFillable, position)
	filled.Fix = fixing(lib, "track_number", "", position)
	return []finding{filled}
}

func genreFinding(ctx context.Context, cat catalog, lib libraryTrack, found catalogTrack) ([]finding, error) {
	if strings.TrimSpace(lib.Genre) != "" {
		return nil, nil
	}
	genres, reason, err := ownAlbumGenres(ctx, cat, lib, found)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return []finding{about(lib, categoryGenreUnfillable, reason)}, nil
	}
	filled := about(lib, categoryGenreFillable, genres[0])
	filled.Fix = fixing(lib, "genre", "", genres[0])
	return []finding{filled}, nil
}

func ownAlbumGenres(ctx context.Context, cat catalog, lib libraryTrack, found catalogTrack) ([]string, string, error) {
	albums, ok := cat.(albumCatalog)
	switch {
	case !sharesLibraryAlbum(lib, found):
		return nil, fmt.Sprintf("catalog album %q is not the library album %q", found.Album, lib.Album), nil
	case found.AlbumID == 0:
		return nil, "catalog track has no album id", nil
	case !ok:
		return nil, "album genres were not consulted", nil
	}
	genres, err := albums.AlbumGenres(ctx, found.AlbumID)
	if err != nil {
		return nil, "album genre lookup failed: " + err.Error(), blockingOnly(err)
	}
	if len(genres) != 1 || strings.TrimSpace(genres[0]) == "" {
		return nil, fmt.Sprintf("catalog album lists %d genres, need exactly one", len(genres)), nil
	}
	return genres, "", nil
}

func resolveOtherReleases(ctx context.Context, cat catalog, lib libraryTrack, findings []finding) ([]finding, error) {
	if !hasCategory(findings, categoryYearOtherRelease) && !hasCategory(findings, categoryAlbumArtistOtherRelease) {
		return findings, nil
	}
	chosen, reason, err := libraryAlbumRelease(ctx, cat, lib)
	if err != nil {
		return nil, err
	}
	resolved := make([]finding, 0, len(findings))
	for _, f := range findings {
		resolved = append(resolved, fromRelease(lib, f, chosen, reason))
	}
	return resolved, nil
}

func fromRelease(lib libraryTrack, f finding, chosen release, reason string) finding {
	if f.Category != categoryYearOtherRelease && f.Category != categoryAlbumArtistOtherRelease {
		return f
	}
	if reason == "" {
		reason = valueFromRelease(f.Category, chosen)
	}
	if reason != "" {
		f.Detail += "; " + reason
		return f
	}
	if f.Category == categoryYearOtherRelease {
		year := chosen.Date[:4]
		filled := about(lib, categoryYearFromRelease, year+" from musicbrainz release "+chosen.ID)
		filled.Fix = fixing(lib, "year", "", year)
		return filled
	}
	filled := about(lib, categoryAlbumArtistFromRelease, chosen.Artist+" from musicbrainz release "+chosen.ID)
	filled.Fix = fixing(lib, "album_artist", lib.AlbumArtist, chosen.Artist)
	return filled
}

func valueFromRelease(kind category, chosen release) string {
	switch {
	case kind == categoryYearOtherRelease && !hasYear(chosen.Date):
		return "musicbrainz release " + chosen.ID + " has no full year"
	case kind == categoryAlbumArtistOtherRelease && strings.TrimSpace(chosen.Artist) == "":
		return "musicbrainz release " + chosen.ID + " has no artist credit"
	}
	return ""
}

func hasYear(date string) bool {
	if len(date) < 4 {
		return false
	}
	year, err := strconv.Atoi(date[:4])
	return err == nil && year > 0
}

func libraryAlbumRelease(ctx context.Context, cat catalog, lib libraryTrack) (release, string, error) {
	releases, ok := cat.(releaseCatalog)
	switch {
	case strings.TrimSpace(lib.Album) == "":
		return release{}, "library track has no album", nil
	case !ok:
		return release{}, "musicbrainz was not consulted", nil
	}
	recordings, err := releases.RecordingsWithReleases(ctx, lib.ISRC)
	if err != nil {
		return release{}, "musicbrainz release lookup failed: " + err.Error(), blockingOnly(err)
	}
	matches := releasesTitled(lib, recordings)
	if len(matches) != 1 {
		return release{}, fmt.Sprintf("%d musicbrainz releases of this recording are titled %q, need exactly one", len(matches), lib.Album), nil
	}
	return matches[0], "", nil
}

func releasesTitled(lib libraryTrack, recordings []recording) []release {
	seen := map[string]bool{}
	var matches []release
	for _, candidate := range recordings {
		if !isSameSong(lib, candidate.asTrack()) {
			continue
		}
		for _, item := range candidate.Releases {
			if seen[item.ID] || textnorm.NormalizeForIdentity(item.Title) != textnorm.NormalizeForIdentity(lib.Album) {
				continue
			}
			seen[item.ID] = true
			matches = append(matches, item)
		}
	}
	return matches
}
