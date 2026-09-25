package service

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func release(title string, recordType domain.RecordType, trackCount int) MergedRelease {
	return MergedRelease{Result: domain.SearchResult{Title: title, TrackCount: trackCount, RecordType: recordType, Extras: map[string]any{}}}
}

func TestNormalizeRecordType(t *testing.T) {
	tests := []struct {
		name       string
		recordType domain.RecordType
		trackCount int
		want       domain.RecordType
	}{
		{"explicit single", "single", 0, "single"},
		{"explicit ep", "ep", 5, "ep"},
		{"explicit album", "album", 12, "album"},
		{"compilation folds into album", "compilation", 20, "album"},
		{"unknown defaults to album", "", 0, "album"},
		{"one-track album is a single", "album", 1, "single"},
		{"one-track unknown is a single", "", 1, "single"},
	}
	for _, tt := range tests {
		got := NormalizeRecordType(release(tt.name, tt.recordType, tt.trackCount))
		if got != tt.want {
			t.Errorf("%s: NormalizeRecordType = %q, want %q", tt.name, got, tt.want)
		}
	}
}
