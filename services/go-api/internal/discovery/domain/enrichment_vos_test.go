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
		// Featured is consumed by the "Featuring" row, not the Deezer detail
		// section — a featured-only enrichment still has no section to show.
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
		// Writers/Copyright without any lyric text render nothing — still zero.
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

func TestEmptyDiscogsEnrichment_NonNilCollections(t *testing.T) {
	e := EmptyDiscogsEnrichment()

	if e.Genres == nil || e.Styles == nil || e.Credits == nil ||
		e.Labels == nil || e.Formats == nil || e.Companies == nil {
		t.Fatalf("EmptyDiscogsEnrichment must have non-nil collections, got %#v", e)
	}
	if !e.IsZero() {
		t.Error("EmptyDiscogsEnrichment must report IsZero")
	}
}

func TestDiscogsEnrichment_IsZero(t *testing.T) {
	tests := []struct {
		name string
		e    DiscogsEnrichment
		want bool
	}{
		{"empty", EmptyDiscogsEnrichment(), true},
		{"master id only", DiscogsEnrichment{MasterID: 1}, false},
		{"genres only", DiscogsEnrichment{Genres: []string{"hip hop"}}, false},
		{"styles only", DiscogsEnrichment{Styles: []string{"conscious"}}, false},
		{"year only", DiscogsEnrichment{Year: 2017}, false},
		{"credits only", DiscogsEnrichment{Credits: []DiscogsCredit{{Name: "Bēkon", Role: "Producer"}}}, false},
		{"labels only", DiscogsEnrichment{Labels: []DiscogsLabelRef{{Name: "TDE", Catno: "B0026716-02"}}}, false},
		{"formats only", DiscogsEnrichment{Formats: []string{"CD · Album"}}, false},
		{"country only", DiscogsEnrichment{Country: "US"}, false},
		{"companies only", DiscogsEnrichment{Companies: []DiscogsCompany{{Name: "Interscope", Role: "Copyright (c)"}}}, false},
		{"community only", DiscogsEnrichment{Community: DiscogsCommunity{Have: 1}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmptyDiscogsArtistEnrichment_NonNilCollections(t *testing.T) {
	e := EmptyDiscogsArtistEnrichment()

	if e.Aliases == nil || e.NameVariations == nil || e.Members == nil ||
		e.Groups == nil || e.Links == nil {
		t.Fatalf("EmptyDiscogsArtistEnrichment must have non-nil collections, got %#v", e)
	}
	if !e.IsZero() {
		t.Error("EmptyDiscogsArtistEnrichment must report IsZero")
	}
}

func TestDiscogsArtistEnrichment_IsZero(t *testing.T) {
	tests := []struct {
		name string
		e    DiscogsArtistEnrichment
		want bool
	}{
		{"empty", EmptyDiscogsArtistEnrichment(), true},
		{"artist id only", DiscogsArtistEnrichment{ArtistID: 1}, false},
		{"profile only", DiscogsArtistEnrichment{Profile: "bio"}, false},
		{"real name only", DiscogsArtistEnrichment{RealName: "Aubrey Graham"}, false},
		{"aliases only", DiscogsArtistEnrichment{Aliases: []string{"Drizzy"}}, false},
		{"name variations only", DiscogsArtistEnrichment{NameVariations: []string{"DRAKE"}}, false},
		{"members only", DiscogsArtistEnrichment{Members: []string{"Thom Yorke"}}, false},
		{"groups only", DiscogsArtistEnrichment{Groups: []string{"Radiohead"}}, false},
		{"links only", DiscogsArtistEnrichment{Links: []DiscogsLink{{Label: "Wikipedia", URL: "https://en.wikipedia.org/x"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.IsZero(); got != tt.want {
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
