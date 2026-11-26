package musicbrainz_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/musicbrainz"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"net/http/httptest"
	"testing"
)

func TestProviderContracts(t *testing.T) {
	providertest.RunContracts(t, []providertest.Contract{
		{
			Name:   "musicbrainz recording",
			Golden: "contract_musicbrainz_recording.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]search.SearchResult, error) {
				return musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(srv.URL), "altune-test/1.0", clock.Now).SearchStructured(t.Context(), "radiohead", "paranoid android", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r search.SearchResult) {
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
			Search: func(t *testing.T, srv *httptest.Server) ([]search.SearchResult, error) {
				return musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(srv.URL), "altune-test/1.0", clock.Now).SearchStructured(t.Context(), "radiohead", "", providertest.ArtistKinds())
			},
			Required: func(t *testing.T, r search.SearchResult) {
				if r.ArtistMBID == "" {
					t.Error("ArtistMBID empty — artist identity resolution consumes it")
				}
			},
		},
	})
}

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want search.ProviderName
	}{
		{(&musicbrainz.MusicBrainzAdapter{}).Name(), domain.ProviderMusicBrainz},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}
