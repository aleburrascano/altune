package domain_test

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func TestParseRecordType_keepsOnlyTheKnownTypesAndIsCaseSensitive(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want domain.RecordType
	}{
		{"album", "album", domain.RecordTypeAlbum},
		{"single", "single", domain.RecordTypeSingle},
		{"ep", "ep", domain.RecordTypeEP},
		{"compilation", "compilation", domain.RecordTypeCompilation},
		{"empty", "", domain.RecordTypeUnknown},
		{"unknown type", "compile", domain.RecordTypeUnknown},
		{"capitalised is not folded", "Album", domain.RecordTypeUnknown},
		{"padded is not trimmed", " ep", domain.RecordTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.ParseRecordType(tt.raw); got != tt.want {
				t.Errorf("ParseRecordType(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRecordType_RankPutsSpecificTypesAboveAlbumAndUnknownLast(t *testing.T) {
	tests := []struct {
		name string
		rt   domain.RecordType
		want int
	}{
		{"single", domain.RecordTypeSingle, 2},
		{"ep", domain.RecordTypeEP, 2},
		{"compilation", domain.RecordTypeCompilation, 2},
		{"album", domain.RecordTypeAlbum, 1},
		{"unknown", domain.RecordTypeUnknown, 0},
		{"unrecognised provider type", domain.RecordType("compile"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rt.Rank(); got != tt.want {
				t.Errorf("RecordType(%q).Rank() = %d, want %d", tt.rt, got, tt.want)
			}
		})
	}
}
