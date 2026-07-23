package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func release(title, recordType string, trackCount int) MergedRelease {
	extras := map[string]any{}
	if recordType != "" {
		extras["record_type"] = recordType
	}
	return MergedRelease{Result: domain.SearchResult{Title: title, TrackCount: trackCount, Extras: extras}}
}

func TestNormalizeRecordType(t *testing.T) {
	tests := []struct {
		name       string
		recordType string
		trackCount int
		want       string
	}{
		{"explicit single", "single", 0, "single"},
		{"explicit ep", "ep", 5, "ep"},
		{"explicit album", "album", 12, "album"},
		{"compilation folds into album", "compilation", 20, "album"},
		{"unknown defaults to album", "", 0, "album"},
		// The user's rule: a one-track release labelled "album" is really a single.
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

func TestBucketDiscography(t *testing.T) {
	buckets := BucketDiscography([]MergedRelease{
		release("Fully Loaded", "ep", 5),
		release("Green Day", "single", 1),
		release("Sayso Says", "album", 10),
		release("Some Comp", "compilation", 30),
		release("Mislabeled", "album", 1), // 1 track → single
	})

	if len(buckets.Albums) != 2 {
		t.Errorf("Albums = %d, want 2 (Sayso Says + Some Comp)", len(buckets.Albums))
	}
	if len(buckets.Singles) != 2 {
		t.Errorf("Singles = %d, want 2 (Green Day + Mislabeled 1-track)", len(buckets.Singles))
	}
	if len(buckets.EPs) != 1 {
		t.Errorf("EPs = %d, want 1 (Fully Loaded)", len(buckets.EPs))
	}
}
