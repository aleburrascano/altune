package lastfm

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
			Name:   "lastfm track",
			Golden: "contract_lastfm_track.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewLastFmAdapter(providertest.NewClient(srv.URL), "test-api-key").Search(t.Context(), "small talk", providertest.TrackKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID == "" {
					t.Error("MBID empty — merge's identifier tier consumes it")
				}
			},
		},
		{
			Name:   "lastfm album",
			Golden: "contract_lastfm_album.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewLastFmAdapter(providertest.NewClient(srv.URL), "test-api-key").Search(t.Context(), "ok computer", providertest.AlbumKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID != "" {
					t.Errorf("album MBID stamped (%q) — Last.fm album-search mbids are RELEASE MBIDs, a different UUID namespace than the RELEASE-GROUP MBIDs MusicBrainz album results carry; stamping one makes the MBID hard-stop block every MB↔Last.fm album merge", r.MBID)
				}
			},
		},
		{
			Name:   "lastfm artist",
			Golden: "contract_lastfm_artist.json",
			Search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewLastFmAdapter(providertest.NewClient(srv.URL), "test-api-key").Search(t.Context(), "the weeknd", providertest.ArtistKinds())
			},
			Required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID == "" {
					t.Error("MBID empty — merge's identifier tier consumes it")
				}
			},
		},
	})
}

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want domain.ProviderName
	}{
		{(&LastFmAdapter{}).Name(), domain.ProviderLastFM},
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
		{"lastfm", (&LastFmAdapter{}).SearchTimeout(), 4 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s SearchTimeout = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}
