package applemusic_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/applemusic"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/search/internal/domain"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"
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
			Name:   "applemusic song",
			Golden: "contract_applemusic_song.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]search.SearchResult, error) {
				return newTestAppleMusicAdapter(t, proxyTo(t, srv)).Search(t.Context(), "blinding lights", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r search.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty — merge's identifier tier consumes it")
				}
				if r.ReleaseDate == "" {
					t.Error("ReleaseDate empty — track detail consumes it")
				}
			},
		},
		{
			Name:   "applemusic album",
			Golden: "contract_applemusic_album.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]search.SearchResult, error) {
				return newTestAppleMusicAdapter(t, proxyTo(t, srv)).Search(t.Context(), "after hours", providertest.AlbumKinds())
			},
			Required: func(t *testing.T, r search.SearchResult) {
				if r.UPC == "" {
					t.Error("UPC empty — merge's album identifier tier consumes it")
				}
				if r.TrackCount == 0 {
					t.Error("TrackCount zero — album detail consumes it")
				}
				if r.ReleaseDate == "" {
					t.Error("ReleaseDate empty — discography ordering consumes it")
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
		{"applemusic", (&applemusic.AppleMusicAdapter{}).SearchTimeout(), 4 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s SearchTimeout = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestAdapterSupportedKinds_allThree(t *testing.T) {
	for name, kinds := range map[string]map[search.ResultKind]bool{
		"applemusic": (&applemusic.AppleMusicAdapter{}).SupportedKinds(),
	} {
		if !kinds[search.ResultKindTrack] || !kinds[search.ResultKindAlbum] || !kinds[search.ResultKindArtist] {
			t.Errorf("%s SupportedKinds = %v, want track+album+artist", name, kinds)
		}
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  domain.ProviderKey
		want string
	}{
		{(&applemusic.AppleMusicAdapter{}).ArtworkSource(), "applemusic"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}
