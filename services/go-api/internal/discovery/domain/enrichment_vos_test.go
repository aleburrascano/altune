package domain

import "testing"

func TestEmptyDeezerEnrichment_NonNilCollections(t *testing.T) {
	e := EmptyDeezerEnrichment()

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
		e    DeezerEnrichment
		want bool
	}{
		{"empty", EmptyDeezerEnrichment(), true},
		{"zero value", DeezerEnrichment{}, true},
		{"bpm only", DeezerEnrichment{BPM: 120}, false},
		{"gain only", DeezerEnrichment{Gain: -7.4}, false},
		{"explicit only", DeezerEnrichment{Explicit: true}, false},
		{"label only", DeezerEnrichment{Label: "Top Dawg"}, false},
		{"genres only", DeezerEnrichment{Genres: []string{"rap"}}, false},
		{"upc only", DeezerEnrichment{UPC: "0602557798456"}, false},
		{"record type only", DeezerEnrichment{RecordType: "ep"}, false},
		{"featured excluded from IsZero", DeezerEnrichment{Featured: []FeaturedArtist{{Name: "SZA"}}}, true},
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
	l := EmptyDeezerLyrics()

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
		l    DeezerLyrics
		want bool
	}{
		{"empty", EmptyDeezerLyrics(), true},
		{"plain only", DeezerLyrics{Plain: "line one"}, false},
		{"synced only", DeezerLyrics{SyncedLines: []SyncedLyricLine{{Line: "line one"}}}, false},
		{"credits only", DeezerLyrics{Writers: []string{"K. Duckworth"}, Copyright: "© 2017"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmptyLastFmEnrichment_NonNilCollections(t *testing.T) {
	e := EmptyLastFmEnrichment()

	if e.Tags == nil || e.Similar == nil {
		t.Fatalf("EmptyLastFmEnrichment must have non-nil collections, got %#v", e)
	}
	if len(e.Tags) != 0 || len(e.Similar) != 0 {
		t.Errorf("EmptyLastFmEnrichment collections must be empty, got %#v", e)
	}
	if !e.IsZero() {
		t.Error("EmptyLastFmEnrichment must report IsZero")
	}
}

func TestLastFmEnrichment_IsZero(t *testing.T) {
	tests := []struct {
		name string
		e    LastFmEnrichment
		want bool
	}{
		{"empty", EmptyLastFmEnrichment(), true},
		{"mbid only", LastFmEnrichment{MBID: "abc"}, false},
		{"listeners only", LastFmEnrichment{Listeners: 1}, false},
		{"playcount only", LastFmEnrichment{Playcount: 1}, false},
		{"tags only", LastFmEnrichment{Tags: []string{"rap"}}, false},
		{"bio only", LastFmEnrichment{Bio: "born in Compton"}, false},
		{"similar only", LastFmEnrichment{Similar: []string{"ScHoolboy Q"}}, false},
		{"duration only", LastFmEnrichment{Duration: 177}, false},
		{"album only", LastFmEnrichment{Album: "DAMN."}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
