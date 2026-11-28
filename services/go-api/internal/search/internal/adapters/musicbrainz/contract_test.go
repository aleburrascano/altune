package musicbrainz_test

import (
	"altune/go-api/internal/search/internal/adapters/musicbrainz"
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"net/http/httptest"
	"testing"
)

func TestProviderContracts(t *testing.T) {
	contracttest.RunContracts(t, []contracttest.Contract{
		{
			Name:   "musicbrainz recording",
			Golden: "contract_musicbrainz_recording.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return musicbrainz.NewMusicBrainzAdapter(contracttest.NewClient(srv.URL), "altune-test/1.0", clock.Now).SearchStructured(t.Context(), "radiohead", "paranoid android", contracttest.TrackKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
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
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return musicbrainz.NewMusicBrainzAdapter(contracttest.NewClient(srv.URL), "altune-test/1.0", clock.Now).SearchStructured(t.Context(), "radiohead", "", contracttest.ArtistKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.ArtistMBID == "" {
					t.Error("ArtistMBID empty — artist identity resolution consumes it")
				}
			},
		},
	})
}

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want domain.ProviderName
	}{
		{(&musicbrainz.MusicBrainzAdapter{}).Name(), domain.ProviderMusicBrainz},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}
