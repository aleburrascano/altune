package service

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

// Pins which xref keys the verifier checks and which content provider serves
// each: an "itunes" xref edge is verified through the Apple Music provider.
func TestVerifiableEdge(t *testing.T) {
	tests := []struct {
		key    string
		want   domain.ProviderName
		wantOK bool
	}{
		{key: "deezer", want: domain.ProviderDeezer, wantOK: true},
		{key: "spotify", want: domain.ProviderSpotify, wantOK: true},
		{key: "itunes", want: domain.ProviderAppleMusic, wantOK: true},
		{key: "applemusic"},
		{key: "discogs"},
		{key: "soundcloud"},
		{key: "wikidata"},
		{key: "musicbrainz"},
		{key: "Deezer"},
		{key: ""},
	}
	for _, tt := range tests {
		got, ok := verifiableEdge(tt.key)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("verifiableEdge(%q) = (%v, %v), want (%v, %v)", tt.key, got, ok, tt.want, tt.wantOK)
		}
	}
}
