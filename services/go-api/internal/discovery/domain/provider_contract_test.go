package domain

import "testing"

func TestRecordTypeVocabulary_ParseFoldsCaseAndPaddingAndMapsOnlyItsOwnWords(t *testing.T) {
	vocabulary := RecordTypeVocabulary{
		"album":  RecordTypeAlbum,
		"single": RecordTypeSingle,
		"ep":     RecordTypeEP,
	}
	tests := []struct {
		name string
		raw  string
		want RecordType
	}{
		{"exact", "album", RecordTypeAlbum},
		{"upper", "SINGLE", RecordTypeSingle},
		{"mixed case", "Ep", RecordTypeEP},
		{"padded", "  album ", RecordTypeAlbum},
		{"word outside this provider's vocabulary", "compilation", RecordTypeUnknown},
		{"empty", "", RecordTypeUnknown},
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
			if got := ValidISRC(tt.raw); got != tt.want {
				t.Errorf("ValidISRC(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPutTypedExtras_ProjectsTheTypedFieldsOntoTheirWireKeys(t *testing.T) {
	r := SearchResult{
		RecordType:    RecordTypeEP,
		ISRC:          "GBARL1600460",
		UPC:           "724384960650",
		Duration:      222,
		TrackPosition: TrackPosition{Disc: 2, Track: 4},
		Explicit:      BoolPtr(true),
		ArtworkPath:   ArtworkPathIdentity,
	}

	extras := map[string]any{}
	PutTypedExtras(extras, r)

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

func TestPutTypedExtras_ANotExplicitResultCarriesNoExplicitKey(t *testing.T) {
	extras := map[string]any{}
	PutTypedExtras(extras, SearchResult{Explicit: BoolPtr(false)})

	if _, present := extras["explicit"]; present {
		t.Errorf("extras = %v, want no explicit key for a clean track", extras)
	}
}

func TestArtworkConfidenceRank_ReadsTheTypedPath(t *testing.T) {
	tests := []struct {
		name string
		path ArtworkPath
		want int
	}{
		{"durable identity", ArtworkPathDurableIdentity, ArtworkRankIdentity},
		{"identity", ArtworkPathIdentity, ArtworkRankIdentity},
		{"name search", ArtworkPathName, ArtworkRankProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := SearchResult{ImageURL: "https://img/x.jpg", ArtworkPath: tt.path}
			if tt.path == ArtworkPathName {
				r.ISRC = "GBARL1600460"
			}
			if got := r.ArtworkConfidenceRank(); got != tt.want {
				t.Errorf("rank = %d, want %d", got, tt.want)
			}
		})
	}
}
