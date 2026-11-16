package deezer_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"net/http/httptest"
	"testing"
)

func TestProviderContracts(t *testing.T) {
	providertest.RunContracts(t, []providertest.Contract{
		{
			Name:   "deezer track",
			Golden: "contract_deezer_track.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]discovery.SearchResult, error) {
				return discovery.NewDeezerAdapter(providertest.NewClient(srv.URL)).Search(t.Context(), "bohemian rhapsody", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r discovery.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty — merge's identifier tier consumes it")
				}
				if r.Duration == 0 {
					t.Error("Duration zero — track detail consumes it")
				}
				if r.Album == "" {
					t.Error("Album empty — identity bridge consumes it")
				}
				if r.DeezerAlbumID == "" {
					t.Error("DeezerAlbumID empty — album-tracks lookup consumes it")
				}
				if r.ProviderRank == 0 {
					t.Error("ProviderRank zero — ranking signal consumes it")
				}
			},
		},
		{
			Name:   "deezer artist",
			Golden: "contract_deezer_artist.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]discovery.SearchResult, error) {
				return discovery.NewDeezerAdapter(providertest.NewClient(srv.URL)).Search(t.Context(), "radiohead", providertest.ArtistKinds())
			},
			Required: func(t *testing.T, r discovery.SearchResult) {
				if r.FanCount == 0 {
					t.Error("FanCount zero — artist prominence consumes it")
				}
			},
		},
		{
			Name:   "deezer album",
			Golden: "contract_deezer_album.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]discovery.SearchResult, error) {
				return discovery.NewDeezerAdapter(providertest.NewClient(srv.URL)).Search(t.Context(), "ok computer", providertest.AlbumKinds())
			},
			Required: func(t *testing.T, r discovery.SearchResult) {
				if r.ReleaseDate == "" {
					t.Error("ReleaseDate empty — discography ordering consumes it")
				}
				if r.TrackCount == 0 {
					t.Error("TrackCount zero — album detail consumes it")
				}
				if r.RecordType == "" {
					t.Error("RecordType missing — discography bucketing consumes it")
				}
			},
		},
	})
}

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want discovery.ProviderName
	}{
		{(&discovery.DeezerAdapter{}).Name(), discovery.ProviderDeezer},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}

func TestAdapterSupportedKinds_allThree(t *testing.T) {
	for name, kinds := range map[string]map[discovery.ResultKind]bool{
		"deezer": (&discovery.DeezerAdapter{}).SupportedKinds(),
	} {
		if !kinds[discovery.ResultKindTrack] || !kinds[discovery.ResultKindAlbum] || !kinds[discovery.ResultKindArtist] {
			t.Errorf("%s SupportedKinds = %v, want track+album+artist", name, kinds)
		}
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  discovery.ProviderKey
		want string
	}{
		{(&discovery.DeezerAdapter{}).ArtworkSource(), "deezer"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}
