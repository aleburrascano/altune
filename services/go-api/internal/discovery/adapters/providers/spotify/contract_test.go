package spotify

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
			Name:   "spotify track",
			Golden: "contract_spotify_track.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestSpotifyAdapter(srv).Search(t.Context(), "sicko mode", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.Extras["explicit"] != true {
					t.Error("Extras[explicit] missing — explicit badge consumes it")
				}
				if r.Duration == 0 {
					t.Error("Duration zero — track detail consumes it")
				}
			},
		},
		{
			Name:   "spotify album",
			Golden: "contract_spotify_album.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestSpotifyAdapter(srv).Search(t.Context(), "after hours", providertest.AlbumKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.Subtitle == "" {
					t.Error("Subtitle empty — canonical title+subtitle merge tier consumes it")
				}
				if r.Year == 0 {
					t.Error("Year zero — discography ordering consumes it")
				}
				if r.RecordType == "" {
					t.Error("RecordType missing — discography bucketing consumes it")
				}
			},
		},
	})
}

func TestAdapterSearchTimeouts(t *testing.T) {
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"spotify", (&SpotifyAdapter{}).SearchTimeout(), spotifySearchTimeout},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s SearchTimeout = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestAdapterSupportedKinds_allThree(t *testing.T) {
	for name, kinds := range map[string]map[domain.ResultKind]bool{
		"spotify": (&SpotifyAdapter{}).SupportedKinds(),
	} {
		if !kinds[domain.ResultKindTrack] || !kinds[domain.ResultKindAlbum] || !kinds[domain.ResultKindArtist] {
			t.Errorf("%s SupportedKinds = %v, want track+album+artist", name, kinds)
		}
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  domain.ProviderKey
		want string
	}{
		{(&SpotifyAdapter{}).ArtworkSource(), "spotify"},
		{(&SpotifyArtworkResolver{}).ArtworkSource(), "spotify"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}
