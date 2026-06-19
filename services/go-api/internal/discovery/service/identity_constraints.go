package service

import (
	"strconv"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

const birthYearOffset = 14

// extractYear pulls the release year from album extras, checking "year"
// then "release_date" fields.
func extractYear(r domain.SearchResult) int {
	if r.Extras == nil {
		return 0
	}
	if v, ok := r.Extras["year"]; ok {
		switch y := v.(type) {
		case int:
			return y
		case float64:
			return int(y)
		case string:
			if n, err := strconv.Atoi(y); err == nil {
				return n
			}
		}
	}
	if v, ok := r.Extras["release_date"]; ok {
		if s, ok := v.(string); ok && len(s) >= 4 {
			if n, err := strconv.Atoi(s[:4]); err == nil {
				return n
			}
		}
	}
	return 0
}

// deezerGenreNames maps common Deezer genre IDs to lowercase genre name strings.
var deezerGenreNames = map[int]string{
	85:  "alternative",
	106: "electro",
	113: "dance",
	116: "rap/hip hop",
	132: "pop",
	152: "rock",
	165: "r&b",
	466: "folk",
	464: "metal",
	129: "jazz",
	98:  "reggae",
	173: "soul & funk",
	153: "blues",
	169: "classical",
	95:  "kids",
	197: "latin",
	2:   "country",
}

// CheckTemporalImpossibility returns true if the album was released before the
// artist could plausibly have been active (birth year + 14).
func CheckTemporalImpossibility(profile domain.ArtistIdentityProfile, album domain.SearchResult) bool {
	if profile.BirthYear == 0 {
		return false
	}
	albumYear := extractYear(album)
	if albumYear == 0 {
		return false
	}
	return albumYear < profile.BirthYear+birthYearOffset
}

// CheckGenreIncompatibility returns true if the album's genre has zero overlap
// with the artist's genre cluster.
func CheckGenreIncompatibility(profile domain.ArtistIdentityProfile, album domain.SearchResult) bool {
	if len(profile.GenreCluster) == 0 {
		return false
	}
	genres := extractAlbumGenres(album)
	if len(genres) == 0 {
		return false
	}
	return !profile.HasGenreOverlap(genres)
}

// CheckArtistTypeMismatch returns true if the profile says "Person" but the
// album's credited artist is a "Group", or vice versa.
func CheckArtistTypeMismatch(profile domain.ArtistIdentityProfile, album domain.SearchResult) bool {
	if profile.ArtistType == "" {
		return false
	}
	if album.Extras == nil {
		return false
	}
	albumType, ok := album.Extras["artist_type"].(string)
	if !ok || albumType == "" {
		return false
	}
	return !strings.EqualFold(profile.ArtistType, albumType)
}

// CheckISRCRegistrantMismatch returns true if the ISRC registrant code doesn't
// match any known registrant in the profile.
func CheckISRCRegistrantMismatch(profile domain.ArtistIdentityProfile, isrc string) bool {
	if len(profile.KnownISRCRegistrants) == 0 {
		return false
	}
	registrant := domain.ExtractISRCRegistrant(isrc)
	if registrant == "" {
		return false
	}
	return !profile.KnownISRCRegistrants[registrant]
}

// extractAlbumGenres pulls genre strings from album extras, splitting on
// "/" and "," to handle composite genres like "Hip-Hop/Rap".
func extractAlbumGenres(album domain.SearchResult) []string {
	if album.Extras == nil {
		return nil
	}

	var raw string

	// Check string genre (from iTunes)
	if g, ok := album.Extras["genre"].(string); ok && g != "" {
		raw = g
	}

	// Check Deezer genre_id (int)
	if raw == "" {
		var genreID int
		switch v := album.Extras["genre_id"].(type) {
		case int:
			genreID = v
		case float64:
			genreID = int(v)
		}
		if genreID > 0 {
			if name, ok := deezerGenreNames[genreID]; ok {
				raw = name
			}
		}
	}

	if raw == "" {
		return nil
	}

	// Split on "/" and "," to handle composite genres
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '/' || r == ','
	})
	genres := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		trimmed = strings.ReplaceAll(trimmed, "-", " ")
		if trimmed != "" {
			genres = append(genres, trimmed)
		}
	}
	return genres
}
