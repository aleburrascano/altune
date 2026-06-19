package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"

	"github.com/stretchr/testify/assert"
)

func TestCheckTemporalImpossibility(t *testing.T) {
	tests := []struct {
		name      string
		birthYear int
		albumYear int
		expected  bool
	}{
		{
			name:      "impossible - album before artist born plus 14",
			birthYear: 2006,
			albumYear: 1995,
			expected:  true,
		},
		{
			name:      "plausible - album well after threshold",
			birthYear: 2006,
			albumYear: 2022,
			expected:  false,
		},
		{
			name:      "borderline ok - exactly at threshold",
			birthYear: 2006,
			albumYear: 2020,
			expected:  false,
		},
		{
			name:      "borderline impossible - one year below threshold",
			birthYear: 2006,
			albumYear: 2019,
			expected:  true,
		},
		{
			name:      "unknown birth year - skip",
			birthYear: 0,
			albumYear: 1995,
			expected:  false,
		},
		{
			name:      "unknown album year - skip",
			birthYear: 2006,
			albumYear: 0,
			expected:  false,
		},
		{
			name:      "both unknown - skip",
			birthYear: 0,
			albumYear: 0,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := domain.NewArtistIdentityProfile()
			profile.BirthYear = tt.birthYear

			extras := map[string]any{}
			if tt.albumYear > 0 {
				extras["year"] = tt.albumYear
			}
			album := domain.SearchResult{
				Kind:   domain.ResultKindAlbum,
				Title:  "Some Album",
				Extras: extras,
			}

			got := CheckTemporalImpossibility(profile, album)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCheckGenreIncompatibility(t *testing.T) {
	tests := []struct {
		name         string
		clusterItems []string
		albumGenre   string   // string genre (iTunes style)
		albumGenreID int      // Deezer genre_id
		expected     bool
	}{
		{
			name:         "incompatible - profile hip-hop, album rock",
			clusterItems: []string{"hip-hop", "rap"},
			albumGenre:   "Rock",
			expected:     true,
		},
		{
			name:         "compatible - profile hip-hop, album hip-hop/rap with slash",
			clusterItems: []string{"hip-hop", "rap"},
			albumGenre:   "Hip-Hop/Rap",
			expected:     false,
		},
		{
			name:         "compatible - profile hip-hop+r&b, album r&b",
			clusterItems: []string{"hip-hop", "r&b"},
			albumGenre:   "R&B",
			expected:     false,
		},
		{
			name:         "incompatible - profile hip-hop only, album classical",
			clusterItems: []string{"hip-hop"},
			albumGenre:   "Classical",
			expected:     true,
		},
		{
			name:         "empty genre cluster - skip",
			clusterItems: []string{},
			albumGenre:   "Rock",
			expected:     false,
		},
		{
			name:         "no genre on album - skip",
			clusterItems: []string{"hip-hop"},
			albumGenre:   "",
			expected:     false,
		},
		{
			name:         "compatible via deezer genre_id - rap/hip hop",
			clusterItems: []string{"rap", "hip hop"},
			albumGenreID: 116, // "rap/hip hop"
			expected:     false,
		},
		{
			name:         "incompatible via deezer genre_id - rock vs hip-hop",
			clusterItems: []string{"hip-hop", "rap"},
			albumGenreID: 152, // "rock"
			expected:     true,
		},
		{
			name:         "unknown deezer genre_id - skip",
			clusterItems: []string{"hip-hop"},
			albumGenreID: 99999,
			expected:     false,
		},
		{
			name:         "comma-separated genre",
			clusterItems: []string{"pop"},
			albumGenre:   "Rock, Pop",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := domain.NewArtistIdentityProfile()
			for _, g := range tt.clusterItems {
				profile.AddGenre(g)
			}

			extras := map[string]any{}
			if tt.albumGenre != "" {
				extras["genre"] = tt.albumGenre
			}
			if tt.albumGenreID > 0 {
				extras["genre_id"] = tt.albumGenreID
			}
			album := domain.SearchResult{
				Kind:   domain.ResultKindAlbum,
				Title:  "Some Album",
				Extras: extras,
			}

			got := CheckGenreIncompatibility(profile, album)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCheckArtistTypeMismatch(t *testing.T) {
	tests := []struct {
		name        string
		profileType string
		albumType   string
		expected    bool
	}{
		{
			name:        "mismatch - person vs group",
			profileType: "Person",
			albumType:   "Group",
			expected:    true,
		},
		{
			name:        "match - person vs person",
			profileType: "Person",
			albumType:   "Person",
			expected:    false,
		},
		{
			name:        "match - case insensitive",
			profileType: "Person",
			albumType:   "person",
			expected:    false,
		},
		{
			name:        "empty profile type - skip",
			profileType: "",
			albumType:   "Group",
			expected:    false,
		},
		{
			name:        "empty album type - skip",
			profileType: "Person",
			albumType:   "",
			expected:    false,
		},
		{
			name:        "no extras on album - skip",
			profileType: "Person",
			albumType:   "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := domain.NewArtistIdentityProfile()
			profile.ArtistType = tt.profileType

			var extras map[string]any
			if tt.albumType != "" {
				extras = map[string]any{"artist_type": tt.albumType}
			}
			album := domain.SearchResult{
				Kind:   domain.ResultKindAlbum,
				Title:  "Some Album",
				Extras: extras,
			}

			got := CheckArtistTypeMismatch(profile, album)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCheckISRCRegistrantMismatch(t *testing.T) {
	tests := []struct {
		name           string
		knownSet       []string
		isrc           string
		expected       bool
	}{
		{
			name:     "match - registrant in known set",
			knownSet: []string{"J842"},
			isrc:     "QZJ842503215",
			expected: false,
		},
		{
			name:     "mismatch - registrant not in known set",
			knownSet: []string{"J842"},
			isrc:     "QZFZ62070654",
			expected: true,
		},
		{
			name:     "mismatch - different registrant",
			knownSet: []string{"J842"},
			isrc:     "CH7812066225",
			expected: true,
		},
		{
			name:     "match - one of multiple known registrants",
			knownSet: []string{"J842", "FZ62"},
			isrc:     "QZFZ62070654",
			expected: false,
		},
		{
			name:     "empty known set - skip",
			knownSet: []string{},
			isrc:     "QZJ842503215",
			expected: false,
		},
		{
			name:     "malformed isrc - skip",
			knownSet: []string{"J842"},
			isrc:     "AB",
			expected: false,
		},
		{
			name:     "empty isrc - skip",
			knownSet: []string{"J842"},
			isrc:     "",
			expected: false,
		},
		{
			name:     "dashed isrc format - match",
			knownSet: []string{"J842"},
			isrc:     "QZ-J84-25-03215",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := domain.NewArtistIdentityProfile()
			for _, r := range tt.knownSet {
				profile.AddISRCRegistrant(r)
			}

			got := CheckISRCRegistrantMismatch(profile, tt.isrc)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractAlbumGenres(t *testing.T) {
	tests := []struct {
		name     string
		extras   map[string]any
		expected []string
	}{
		{
			name:     "simple string genre",
			extras:   map[string]any{"genre": "Rock"},
			expected: []string{"Rock"},
		},
		{
			name:     "slash-separated genre with hyphen normalization",
			extras:   map[string]any{"genre": "Hip-Hop/Rap"},
			expected: []string{"Hip Hop", "Rap"},
		},
		{
			name:     "comma-separated genre",
			extras:   map[string]any{"genre": "Rock, Pop, Indie"},
			expected: []string{"Rock", "Pop", "Indie"},
		},
		{
			name:     "deezer genre_id int",
			extras:   map[string]any{"genre_id": 116},
			expected: []string{"rap", "hip hop"},
		},
		{
			name:     "deezer genre_id float64",
			extras:   map[string]any{"genre_id": float64(152)},
			expected: []string{"rock"},
		},
		{
			name:     "nil extras",
			extras:   nil,
			expected: nil,
		},
		{
			name:     "empty genre string",
			extras:   map[string]any{"genre": ""},
			expected: nil,
		},
		{
			name:     "string genre takes priority over genre_id",
			extras:   map[string]any{"genre": "Pop", "genre_id": 152},
			expected: []string{"Pop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			album := domain.SearchResult{Extras: tt.extras}
			got := extractAlbumGenres(album)
			assert.Equal(t, tt.expected, got)
		})
	}
}
