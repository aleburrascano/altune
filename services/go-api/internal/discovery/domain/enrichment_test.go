package domain_test

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func TestEmptyEnrichment_NonNilCollections(t *testing.T) {
	e := domain.EmptyEnrichment()

	if e.Genres == nil || e.SecondaryTypes == nil || e.ExternalIDs == nil {
		t.Fatalf("EmptyEnrichment must have non-nil collections, got %#v", e)
	}
	if len(e.Genres) != 0 || len(e.SecondaryTypes) != 0 || len(e.ExternalIDs) != 0 {
		t.Errorf("EmptyEnrichment collections must be empty, got %#v", e)
	}
	if !e.IsZero() {
		t.Error("EmptyEnrichment must report IsZero")
	}
}

func TestMBEnrichment_IsZero(t *testing.T) {
	tests := []struct {
		name string
		e    domain.MBEnrichment
		want bool
	}{
		{"empty", domain.EmptyEnrichment(), true},
		{"mbid only", domain.MBEnrichment{MBID: "abc", Genres: []string{}, SecondaryTypes: []string{}, ExternalIDs: map[domain.ProviderKey]string{}}, false},
		{"genres only", domain.MBEnrichment{Genres: []string{"hip hop"}}, false},
		{"artwork only", domain.MBEnrichment{ArtworkURL: "https://x/y.jpg"}, false},
		{"year only", domain.MBEnrichment{Year: 2017}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmptyDeezerEnrichment_NonNilCollections(t *testing.T) {
	e := domain.EmptyDeezerEnrichment()

	if e.Genres == nil {
		t.Fatalf("EmptyDeezerEnrichment must have a non-nil Genres slice, got %#v", e)
	}
	if len(e.Genres) != 0 {
		t.Errorf("EmptyDeezerEnrichment Genres must be empty, got %#v", e.Genres)
	}
	if !e.IsZero() {
		t.Error("EmptyDeezerEnrichment must report IsZero")
	}
}

func TestDeezerEnrichment_IsZero(t *testing.T) {
	tests := []struct {
		name string
		e    domain.DeezerEnrichment
		want bool
	}{
		{"empty", domain.EmptyDeezerEnrichment(), true},
		{"zero value", domain.DeezerEnrichment{}, true},
		{"bpm only", domain.DeezerEnrichment{BPM: 120}, false},
		{"gain only", domain.DeezerEnrichment{Gain: -7.4}, false},
		{"explicit only", domain.DeezerEnrichment{Explicit: true}, false},
		{"label only", domain.DeezerEnrichment{Label: "Top Dawg"}, false},
		{"genres only", domain.DeezerEnrichment{Genres: []string{"rap"}}, false},
		{"upc only", domain.DeezerEnrichment{UPC: "0602557798456"}, false},
		{"record type only", domain.DeezerEnrichment{RecordType: "ep"}, false},
		{"featured excluded from IsZero", domain.DeezerEnrichment{Featured: []domain.FeaturedArtist{{Name: "SZA"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmptyDeezerLyrics_NonNilCollections(t *testing.T) {
	l := domain.EmptyDeezerLyrics()

	if l.SyncedLines == nil || l.Writers == nil {
		t.Fatalf("EmptyDeezerLyrics must have non-nil collections, got %#v", l)
	}
	if len(l.SyncedLines) != 0 || len(l.Writers) != 0 {
		t.Errorf("EmptyDeezerLyrics collections must be empty, got %#v", l)
	}
	if !l.IsZero() {
		t.Error("EmptyDeezerLyrics must report IsZero")
	}
}

func TestDeezerLyrics_IsZero(t *testing.T) {
	tests := []struct {
		name string
		l    domain.DeezerLyrics
		want bool
	}{
		{"empty", domain.EmptyDeezerLyrics(), true},
		{"plain only", domain.DeezerLyrics{Plain: "line one"}, false},
		{"synced only", domain.DeezerLyrics{SyncedLines: []domain.SyncedLyricLine{{Line: "line one"}}}, false},
		{"credits only", domain.DeezerLyrics{Writers: []string{"K. Duckworth"}, Copyright: "© 2017"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
