package musicbrainz_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"net/http/httptest"
	"testing"
)

func TestProviderContracts(t *testing.T) {
	providertest.RunContracts(t, []providertest.Contract{
		{
			Name:   "musicbrainz recording",
			Golden: "contract_musicbrainz_recording.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]discovery.SearchResult, error) {
				return discovery.NewMusicBrainzAdapter(providertest.NewClient(srv.URL), "altune-test/1.0").SearchStructured(t.Context(), "radiohead", "paranoid android", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r discovery.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty — merge's identifier tier consumes it")
				}
				if r.RecordingMBID == "" {
					t.Error("RecordingMBID empty — merge's identifier tier consumes it")
				}
			},
		},
		{
			Name:   "musicbrainz artist",
			Golden: "contract_musicbrainz_artist.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]discovery.SearchResult, error) {
				return discovery.NewMusicBrainzAdapter(providertest.NewClient(srv.URL), "altune-test/1.0").SearchStructured(t.Context(), "radiohead", "", providertest.ArtistKinds())
			},
			Required: func(t *testing.T, r discovery.SearchResult) {
				if r.ArtistMBID == "" {
					t.Error("ArtistMBID empty — artist identity resolution consumes it")
				}
			},
		},
	})
}

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want discovery.ProviderName
	}{
		{(&discovery.MusicBrainzAdapter{}).Name(), discovery.ProviderMusicBrainz},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}
