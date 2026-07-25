package providers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func albumKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindAlbum: true}
}

func artistKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindArtist: true}
}

type providerContract struct {
	name     string
	golden   string
	search   func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error)
	required func(t *testing.T, r domain.SearchResult)
}

func TestProviderContracts(t *testing.T) {
	cases := []providerContract{
		{
			name:   "deezer track",
			golden: "contract_deezer_track.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewDeezerAdapter(newTestClient(srv.URL)).Search(t.Context(), "bohemian rhapsody", trackKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
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
			name:   "deezer artist",
			golden: "contract_deezer_artist.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewDeezerAdapter(newTestClient(srv.URL)).Search(t.Context(), "radiohead", artistKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.FanCount == 0 {
					t.Error("FanCount zero — artist prominence consumes it")
				}
			},
		},
		{
			name:   "deezer album",
			golden: "contract_deezer_album.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewDeezerAdapter(newTestClient(srv.URL)).Search(t.Context(), "ok computer", albumKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.ReleaseDate == "" {
					t.Error("ReleaseDate empty — discography ordering consumes it")
				}
				if r.TrackCount == 0 {
					t.Error("TrackCount zero — album detail consumes it")
				}
				if r.Extras["record_type"] == nil {
					t.Error("Extras[record_type] missing — discography bucketing consumes it")
				}
			},
		},
		{
			name:   "applemusic song",
			golden: "contract_applemusic_song.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestAppleMusicAdapter(srv).Search(t.Context(), "blinding lights", trackKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty — merge's identifier tier consumes it")
				}
				if r.ReleaseDate == "" {
					t.Error("ReleaseDate empty — track detail consumes it")
				}
			},
		},
		{
			name:   "applemusic album",
			golden: "contract_applemusic_album.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestAppleMusicAdapter(srv).Search(t.Context(), "after hours", albumKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.UPC == "" {
					t.Error("UPC empty — merge's album identifier tier consumes it")
				}
				if r.TrackCount == 0 {
					t.Error("TrackCount zero — album detail consumes it")
				}
				if r.ReleaseDate == "" {
					t.Error("ReleaseDate empty — discography ordering consumes it")
				}
				if r.Extras["record_type"] == nil {
					t.Error("Extras[record_type] missing — discography bucketing consumes it")
				}
			},
		},
		{
			name:   "lastfm track",
			golden: "contract_lastfm_track.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewLastFmAdapter(newTestClient(srv.URL), "test-api-key").Search(t.Context(), "small talk", trackKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID == "" {
					t.Error("MBID empty — merge's identifier tier consumes it")
				}
			},
		},
		{
			name:   "lastfm album",
			golden: "contract_lastfm_album.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewLastFmAdapter(newTestClient(srv.URL), "test-api-key").Search(t.Context(), "ok computer", albumKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID != "" {
					t.Errorf("album MBID stamped (%q) — Last.fm album-search mbids are RELEASE MBIDs, a different UUID namespace than the RELEASE-GROUP MBIDs MusicBrainz album results carry; stamping one makes the MBID hard-stop block every MB↔Last.fm album merge", r.MBID)
				}
			},
		},
		{
			name:   "lastfm artist",
			golden: "contract_lastfm_artist.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewLastFmAdapter(newTestClient(srv.URL), "test-api-key").Search(t.Context(), "the weeknd", artistKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID == "" {
					t.Error("MBID empty — merge's identifier tier consumes it")
				}
			},
		},
		{
			name:   "musicbrainz recording",
			golden: "contract_musicbrainz_recording.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewMusicBrainzAdapter(newTestClient(srv.URL), "altune-test/1.0").Search(t.Context(), "paranoid android", trackKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty — merge's identifier tier consumes it")
				}
				if r.MBID == "" {
					t.Error("MBID empty — merge's identifier tier consumes it")
				}
			},
		},
		{
			name:   "musicbrainz artist",
			golden: "contract_musicbrainz_artist.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return NewMusicBrainzAdapter(newTestClient(srv.URL), "altune-test/1.0").Search(t.Context(), "radiohead", artistKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.MBID == "" {
					t.Error("MBID empty — artist identity resolution consumes it")
				}
			},
		},
		{
			name:   "spotify track",
			golden: "contract_spotify_track.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestSpotifyAdapter(srv).Search(t.Context(), "sicko mode", trackKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.Extras["explicit"] != true {
					t.Error("Extras[explicit] missing — explicit badge consumes it")
				}
				if r.Duration == 0 {
					t.Error("Duration zero — track detail consumes it")
				}
			},
		},
		{
			name:   "spotify album",
			golden: "contract_spotify_album.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestSpotifyAdapter(srv).Search(t.Context(), "after hours", albumKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.Subtitle == "" {
					t.Error("Subtitle empty — canonical title+subtitle merge tier consumes it")
				}
				if r.Year == 0 {
					t.Error("Year zero — discography ordering consumes it")
				}
				if r.Extras["record_type"] == nil {
					t.Error("Extras[record_type] missing — discography bucketing consumes it")
				}
			},
		},
		{
			name:   "soundcloud track",
			golden: "contract_soundcloud_track.json",
			search: func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error) {
				return newTestSoundCloudAPI(srv, nil).Search(t.Context(), "goosebumps", trackKinds())
			},
			required: func(t *testing.T, r domain.SearchResult) {
				if r.ISRC == "" {
					t.Error("ISRC empty (publisher_metadata) — merge's identifier tier consumes it")
				}
				if r.Album == "" {
					t.Error("Album empty (publisher_metadata) — identity bridge consumes it")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			if err != nil {
				t.Fatalf("read golden %s: %v", tc.golden, err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(golden)
			}))
			defer srv.Close()

			results, err := tc.search(t, srv)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected exactly 1 mapped result from the golden, got %d", len(results))
			}
			tc.required(t, results[0])
		})
	}
}
