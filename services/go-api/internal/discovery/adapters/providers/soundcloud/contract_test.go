package soundcloud

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/domain"
	"net/http/httptest"
	"testing"
)

func TestProviderContracts(t *testing.T) {
	providertest.RunContracts(t, []providertest.Contract{
		{
			Name:   "soundcloud track",
			Golden: "contract_soundcloud_track.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestSoundCloudAPI(srv, nil).Search(t.Context(), "goosebumps", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty (publisher_metadata) — merge's identifier tier consumes it")
				}
				if r.Album == "" {
					t.Error("Album empty (publisher_metadata) — identity bridge consumes it")
				}
			},
		},
	})
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  domain.ProviderKey
		want string
	}{
		{(&SoundCloudAPIAdapter{}).ArtworkSource(), "soundcloud"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}
