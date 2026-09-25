package domain

import "testing"

// Providers stamp their raw release type into SearchResult.RecordType, so the
// parse must stay exactly this lenient: no case folding, no trimming, and an
// unrecognised type collapsing to the unknown zero value rather than surviving.
func TestParseRecordType_keepsOnlyTheKnownTypesAndIsCaseSensitive(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want RecordType
	}{
		{"album", "album", RecordTypeAlbum},
		{"single", "single", RecordTypeSingle},
		{"ep", "ep", RecordTypeEP},
		{"compilation", "compilation", RecordTypeCompilation},
		{"empty", "", RecordTypeUnknown},
		{"unknown type", "compile", RecordTypeUnknown},
		{"capitalised is not folded", "Album", RecordTypeUnknown},
		{"padded is not trimmed", " ep", RecordTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseRecordType(tt.raw); got != tt.want {
				t.Errorf("ParseRecordType(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// Release merge keeps the higher-ranked type of two variants, so a single, an
// EP and a compilation all have to outrank a plain album, and an unknown type
// has to lose to every known one.
func TestRecordType_RankPutsSpecificTypesAboveAlbumAndUnknownLast(t *testing.T) {
	tests := []struct {
		name string
		rt   RecordType
		want int
	}{
		{"single", RecordTypeSingle, 2},
		{"ep", RecordTypeEP, 2},
		{"compilation", RecordTypeCompilation, 2},
		{"album", RecordTypeAlbum, 1},
		{"unknown", RecordTypeUnknown, 0},
		{"unrecognised provider type", RecordType("compile"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rt.Rank(); got != tt.want {
				t.Errorf("RecordType(%q).Rank() = %d, want %d", tt.rt, got, tt.want)
			}
		})
	}
}
