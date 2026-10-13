package main

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const lengthToleranceSeconds = 10.0

type category string

const (
	categoryWrongSong           category = "wrong_song"
	categoryLengthShorter       category = "length_shorter"
	categoryLengthLonger        category = "length_longer"
	categoryYearFillable        category = "year_fillable"
	categoryAlbumArtistFillable category = "album_artist_fillable"
	categoryISRCFillable        category = "isrc_fillable"
	categoryISRCAmbiguous       category = "isrc_ambiguous"
	categoryCoverURLDiffers     category = "cover_url_differs"
	categoryCoverImageDiffers   category = "cover_image_differs"
	categoryCoverUnreadable     category = "cover_unreadable"
	categoryNoISRC              category = "no_isrc"
	categoryUnknownISRC         category = "unknown_isrc"
	categoryLookupFailed        category = "lookup_failed"
)

type libraryTrack struct {
	ID              string
	UserID          string
	Title           string
	Artist          string
	Album           string
	AlbumArtist     string
	ISRC            string
	ArtworkURL      string
	Year            int
	DurationSeconds float64
}

type finding struct {
	TrackID  string   `json:"track_id"`
	Artist   string   `json:"artist"`
	Title    string   `json:"title"`
	Category category `json:"category"`
	Detail   string   `json:"detail,omitempty"`
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
	return titlesAgree(lib.Title, cat.Title) && artistsAgree(lib.Artist, cat)
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
		return []finding{about(lib, categoryWrongSong, "catalog says "+cat.Artist+" - "+cat.Title)}
	}
	var found []finding
	if kind, off := lengthCategory(lib, cat); off {
		found = append(found, about(lib, kind, lengthDetail(lib, cat)))
	}
	if lib.Year == 0 && cat.ReleaseYear > 0 {
		found = append(found, about(lib, categoryYearFillable, strconv.Itoa(cat.ReleaseYear)))
	}
	if strings.TrimSpace(lib.AlbumArtist) == "" && cat.Artist != "" {
		found = append(found, about(lib, categoryAlbumArtistFillable, cat.Artist))
	}
	return found
}

func about(lib libraryTrack, kind category, detail string) finding {
	return finding{TrackID: lib.ID, Artist: lib.Artist, Title: lib.Title, Category: kind, Detail: detail}
}

func lengthDetail(lib libraryTrack, cat catalogTrack) string {
	return strconv.Itoa(int(lib.DurationSeconds)) + "s audio vs " + strconv.Itoa(int(cat.DurationSeconds)) + "s catalog"
}
