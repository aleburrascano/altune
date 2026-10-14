package main

import (
	"altune/go-api/internal/acquisition/service/versiontag"
	"altune/go-api/internal/shared/textnorm"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	lengthToleranceSeconds = 10.0
	earliestPlausibleYear  = 1900
)

func latestPlausibleYear() int { return time.Now().Year() + 1 }

func isPlausibleYear(year int) bool {
	return year >= earliestPlausibleYear && year <= latestPlausibleYear()
}

type category string

const (
	categoryWrongSong                 category = "wrong_song"
	categoryLengthShorter             category = "length_shorter"
	categoryLengthLonger              category = "length_longer"
	categoryYearFillable              category = "year_fillable"
	categoryAlbumArtistFillable       category = "album_artist_fillable"
	categoryISRCFillable              category = "isrc_fillable"
	categoryYearOtherRelease          category = "year_other_release"
	categoryAlbumArtistOtherRelease   category = "album_artist_other_release"
	categoryISRCAmbiguous             category = "isrc_ambiguous"
	categoryCoverURLDiffers           category = "cover_url_differs"
	categoryCoverImageDiffers         category = "cover_image_differs"
	categoryCoverUnreadable           category = "cover_unreadable"
	categoryNoISRC                    category = "no_isrc"
	categoryUnknownISRC               category = "unknown_isrc"
	categoryISRCConfirmed             category = "isrc_confirmed"
	categoryTrackNumberFillable       category = "track_number_fillable"
	categoryTrackNumberUnfillable     category = "track_number_unfillable"
	categoryGenreFillable             category = "genre_fillable"
	categoryGenreUnfillable           category = "genre_unfillable"
	categoryCatalogDurationFillable   category = "catalog_duration_fillable"
	categoryCoverBlank                category = "cover_blank"
	categoryCoverNotReplaceable       category = "cover_not_replaceable"
	categoryYearFromRelease           category = "year_from_release"
	categoryAlbumArtistFromRelease    category = "album_artist_from_release"
	categoryLookupFailed              category = "lookup_failed"
	categoryYearUnfillable            category = "year_unfillable"
	categoryCatalogDurationUnfillable category = "catalog_duration_unfillable"
)

type libraryTrack struct {
	ID                     string
	UserID                 string
	Title                  string
	Artist                 string
	Album                  string
	AlbumArtist            string
	ISRC                   string
	ArtworkURL             string
	Year                   int
	Genre                  string
	TrackNumber            int
	CatalogDurationSeconds float64
	DurationSeconds        float64
	Version                int
}

type finding struct {
	TrackID  string   `json:"track_id"`
	Artist   string   `json:"artist"`
	Title    string   `json:"title"`
	Category category `json:"category"`
	Detail   string   `json:"detail,omitempty"`
	Fix      *fix     `json:"fix,omitempty"`

	LengthPlan *lengthPlan `json:"length_plan,omitempty"`
}

type fix struct {
	UserID   string   `json:"user_id"`
	Version  int      `json:"version"`
	Field    string   `json:"field"`
	OldValue *string  `json:"old_value"`
	NewValue *string  `json:"new_value"`
	Sources  []string `json:"sources,omitempty"`
}

func normalizeName(s string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func baseTitle(s string) string {
	for _, open := range []string{" (", " [", " - "} {
		if i := strings.Index(s, open); i > 0 {
			s = s[:i]
		}
	}
	return s
}

func foldedKey(s string) string {
	return strings.TrimSpace(cases.Fold().String(norm.NFKC.String(s)))
}

func namesAgree(a, b string) bool {
	na, nb := normalizeName(a), normalizeName(b)
	if na == "" || nb == "" {
		ka, kb := foldedKey(a), foldedKey(b)
		return ka != "" && ka == kb
	}
	return na == nb || strings.Contains(na, nb) || strings.Contains(nb, na)
}

func titlesAgree(library, catalog string) bool {
	return namesAgree(baseTitle(library), baseTitle(catalog))
}

func artistsAgree(library string, catalog catalogTrack) bool {
	if namesAgree(library, catalog.Artist) {
		return true
	}
	for _, contributor := range catalog.Contributors {
		if namesAgree(library, contributor) {
			return true
		}
	}
	return false
}

func isSameSong(lib libraryTrack, cat catalogTrack) bool {
	return titlesAgree(lib.Title, cat.Title) && versionsAgree(lib.Title, cat.Title) && artistsAgree(lib.Artist, cat)
}

func versionsAgree(library, catalog string) bool {
	return slices.Equal(versiontag.RecordingLabels(library), versiontag.RecordingLabels(catalog))
}

func fitsLength(lib libraryTrack, cat catalogTrack) bool {
	if lib.DurationSeconds <= 0 || cat.DurationSeconds <= 0 {
		return false
	}
	return math.Abs(lib.DurationSeconds-cat.DurationSeconds) <= lengthToleranceSeconds
}

func lengthCategory(lib libraryTrack, cat catalogTrack) (category, bool) {
	if lib.DurationSeconds <= 0 || cat.DurationSeconds <= 0 {
		return "", false
	}
	gap := lib.DurationSeconds - cat.DurationSeconds
	switch {
	case math.Abs(gap) <= lengthToleranceSeconds:
		return "", false
	case gap < 0:
		return categoryLengthShorter, true
	default:
		return categoryLengthLonger, true
	}
}

func metadataFindings(lib libraryTrack, cat catalogTrack) []finding {
	if !isSameSong(lib, cat) {
		wrong := about(lib, categoryWrongSong, "catalog says "+cat.Artist+" - "+cat.Title)
		wrong.Fix = fixing(lib, "isrc", lib.ISRC, "")
		return []finding{wrong}
	}
	var found []finding
	if kind, off := lengthCategory(lib, cat); off {
		found = append(found, about(lib, kind, lengthDetail(lib, cat)))
	}
	sameAlbum := textnorm.NormalizeForIdentity(lib.Album) == textnorm.NormalizeForIdentity(cat.Album)
	if lib.Year == 0 && cat.ReleaseYear > 0 && sameAlbum && !isPlausibleYear(cat.ReleaseYear) {
		found = append(found, about(lib, categoryYearUnfillable, fmt.Sprintf("catalog year %d is outside %d to %d", cat.ReleaseYear, earliestPlausibleYear, latestPlausibleYear())))
	} else if lib.Year == 0 && cat.ReleaseYear > 0 {
		found = append(found, fillFinding(lib, cat, sameAlbum, categoryYearFillable, categoryYearOtherRelease, "year", "", strconv.Itoa(cat.ReleaseYear)))
	}
	if strings.TrimSpace(lib.AlbumArtist) == "" && cat.Artist != "" {
		found = append(found, fillFinding(lib, cat, sameAlbum, categoryAlbumArtistFillable, categoryAlbumArtistOtherRelease, "album_artist", lib.AlbumArtist, cat.Artist))
	}
	return found
}

func fillFinding(lib libraryTrack, cat catalogTrack, sameAlbum bool, fillable, otherRelease category, fieldName, oldValue, newValue string) finding {
	if !sameAlbum {
		return about(lib, otherRelease, newValue+" from catalog album "+strconv.Quote(cat.Album)+" vs library album "+strconv.Quote(lib.Album))
	}
	filled := about(lib, fillable, newValue)
	filled.Fix = fixing(lib, fieldName, oldValue, newValue)
	return filled
}

func fixing(lib libraryTrack, field, oldValue, newValue string) *fix {
	return &fix{UserID: lib.UserID, Version: lib.Version, Field: field, OldValue: nullIfEmpty(oldValue), NewValue: nullIfEmpty(newValue)}
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func about(lib libraryTrack, kind category, detail string) finding {
	return finding{TrackID: lib.ID, Artist: lib.Artist, Title: lib.Title, Category: kind, Detail: detail}
}

func lengthDetail(lib libraryTrack, cat catalogTrack) string {
	return strconv.Itoa(int(lib.DurationSeconds)) + "s audio vs " + strconv.Itoa(int(cat.DurationSeconds)) + "s catalog"
}
