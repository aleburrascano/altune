package soundcloud_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func proxyTo(t *testing.T, srv *httptest.Server) http.HandlerFunc {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse contract server URL: %v", err)
	}
	return httputil.NewSingleHostReverseProxy(target).ServeHTTP
}

func TestProviderContracts(t *testing.T) {
	providertest.RunContracts(t, []providertest.Contract{
		{
			Name:   "soundcloud track",
			Golden: "contract_soundcloud_track.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]discovery.SearchResult, error) {
				a := newTestSoundCloudAPI(t, proxyTo(t, srv), nil)
				return a.Search(t.Context(), "goosebumps", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r discovery.SearchResult) {
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
		got  discovery.ProviderKey
		want string
	}{
		{(&discovery.SoundCloudAPIAdapter{}).ArtworkSource(), "soundcloud"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}
