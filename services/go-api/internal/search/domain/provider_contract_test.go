package domain_test

import (
	"altune/go-api/internal/search/domain"
	"testing"
)

func TestRecordTypeVocabulary_ParseFoldsCaseAndPaddingAndMapsOnlyItsOwnWords(t *testing.T) {
	vocabulary := domain.RecordTypeVocabulary{
		"album":  domain.RecordTypeAlbum,
		"single": domain.RecordTypeSingle,
		"ep":     domain.RecordTypeEP,
	}
	tests := []struct {
		name string
		raw  string
		want domain.RecordType
	}{
		{"exact", "album", domain.RecordTypeAlbum},
		{"upper", "SINGLE", domain.RecordTypeSingle},
		{"mixed case", "Ep", domain.RecordTypeEP},
		{"padded", "  album ", domain.RecordTypeAlbum},
		{"word outside this provider's vocabulary", "compilation", domain.RecordTypeUnknown},
		{"empty", "", domain.RecordTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vocabulary.Parse(tt.raw); got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestValidISRC_ReturnsTheCanonicalFormOrEmpty(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"already canonical", "GBARL1600460", "GBARL1600460"},
		{"dashed lower case", "gb-arl-16-00460", "GBARL1600460"},
		{"padded", " GBARL1600460 ", "GBARL1600460"},
		{"too short", "GBARL16004", ""},
		{"not alphanumeric", "GBARL16004!0", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.ValidISRC(tt.raw); got != tt.want {
				t.Errorf("ValidISRC(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPutTypedExtras_ProjectsTheTypedFieldsOntoTheirWireKeys(t *testing.T) {
	r := domain.SearchResult{
		RecordType:    domain.RecordTypeEP,
		ISRC:          "GBARL1600460",
		UPC:           "724384960650",
		Duration:      222,
		TrackPosition: domain.TrackPosition{Disc: 2, Track: 4},
		Explicit:      domain.BoolPtr(true),
		ArtworkPath:   domain.ArtworkPathIdentity,
	}

	extras := map[string]any{}
	domain.PutTypedExtras(extras, r)

	want := map[string]any{
		"record_type":  "ep",
		"isrc":         "GBARL1600460",
		"upc":          "724384960650",
		"duration":     222,
		"track_number": 4,
		"disc_number":  2,
		"explicit":     true,
		"artwork_path": "identity",
	}
	for key, value := range want {
		if extras[key] != value {
			t.Errorf("extras[%q] = %v, want %v", key, extras[key], value)
		}
	}
	if len(extras) != len(want) {
		t.Errorf("extras = %v, want exactly %v", extras, want)
	}
}

func TestArtworkConfidenceRank_ReadsTheTypedPath(t *testing.T) {
	tests := []struct {
		name string
		path domain.ArtworkPath
		want int
	}{
		{"durable identity", domain.ArtworkPathDurableIdentity, domain.ArtworkRankIdentity},
		{"identity", domain.ArtworkPathIdentity, domain.ArtworkRankIdentity},
		{"name search", domain.ArtworkPathName, domain.ArtworkRankProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.SearchResult{ImageURL: "https://img/x.jpg", ArtworkPath: tt.path}
			if tt.path == domain.ArtworkPathName {
				r.ISRC = "GBARL1600460"
			}
			if got := r.ArtworkConfidenceRank(); got != tt.want {
				t.Errorf("rank = %d, want %d", got, tt.want)
			}
		})
	}
}
