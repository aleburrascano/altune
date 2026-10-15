package musicbrainz

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/domain"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProviderContracts(t *testing.T) {
	providertest.RunContracts(t, []providertest.Contract{
		{
			Name:   "musicbrainz recording",
			Golden: "contract_musicbrainz_recording.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewMusicBrainzAdapter(providertest.NewClient(srv.URL), "altune-test/1.0").Search(t.Context(), "paranoid android", providertest.TrackKinds())
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
				return NewMusicBrainzAdapter(providertest.NewClient(srv.URL), "altune-test/1.0").Search(t.Context(), "radiohead", providertest.ArtistKinds())
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
		{(&MusicBrainzAdapter{}).Name(), domain.ProviderMusicBrainz},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}

func TestAdapterSearchTimeouts(t *testing.T) {
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"musicbrainz", (&MusicBrainzAdapter{}).SearchTimeout(), 5 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s SearchTimeout = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestAdapterSupportedKinds_allThree(t *testing.T) {
	for name, kinds := range map[string]map[domain.ResultKind]bool{
		"musicbrainz": (&MusicBrainzAdapter{}).SupportedKinds(),
	} {
		if !kinds[domain.ResultKindTrack] || !kinds[domain.ResultKindAlbum] || !kinds[domain.ResultKindArtist] {
			t.Errorf("%s SupportedKinds = %v, want track+album+artist", name, kinds)
		}
	}
}
