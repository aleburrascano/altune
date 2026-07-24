package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

const lastfmTestMBID = "a74b1b7f-71a5-4011-9441-d0b5e4122711"

func TestLooksLikeMBID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{lastfmTestMBID, true},
		{strings.ToUpper(lastfmTestMBID), true},
		{"the weeknd", false},
		{"a74b1b7f-71a5-4011-9441-d0b5e412271", false},   // 35 chars
		{"a74b1b7f_71a5_4011_9441_d0b5e4122711", false},  // wrong separators
		{"g74b1b7f-71a5-4011-9441-d0b5e4122711", false},  // non-hex
	}
	for _, tt := range tests {
		if got := looksLikeMBID(tt.in); got != tt.want {
			t.Errorf("looksLikeMBID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestLastFmAdapter_GetArtistTopTracks(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"toptracks": {"track": [{
			"name": "Blinding Lights",
			"playcount": "2900000",
			"listeners": "1,500,000",
			"url": "https://www.last.fm/music/The+Weeknd/_/Blinding+Lights",
			"artist": {"name": "The Weeknd"},
			"image": [
				{"#text": "https://img/small.png", "size": "small"},
				{"#text": "https://img/xl.png", "size": "extralarge"}
			]
		}]}}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-key")

	t.Run("by name uses artist param", func(t *testing.T) {
		results, err := adapter.GetArtistTopTracks(context.Background(), domain.ProviderLastFM, "The Weeknd")
		if err != nil {
			t.Fatalf("GetArtistTopTracks: %v", err)
		}
		if !strings.Contains(gotQuery, "artist=The+Weeknd") || strings.Contains(gotQuery, "mbid=") {
			t.Errorf("query = %q, want artist= (not mbid=) for a plain name", gotQuery)
		}
		if len(results) != 1 {
			t.Fatalf("results = %d, want 1", len(results))
		}
		r := results[0]
		if r.Title != "Blinding Lights" || r.Subtitle != "The Weeknd" {
			t.Errorf("result = %+v", r)
		}
		if r.ImageURL != "https://img/xl.png" {
			t.Errorf("ImageURL = %q, want the extralarge variant", r.ImageURL)
		}
		if r.Extras["playcount"] != int64(2900000) {
			t.Errorf("playcount = %v, want int64(2900000)", r.Extras["playcount"])
		}
		if r.Extras["listeners"] != int64(1500000) {
			t.Errorf("listeners = %v, want commas stripped to 1500000", r.Extras["listeners"])
		}
		if r.Sources[0].ExternalID != "The+Weeknd/_/Blinding+Lights" {
			t.Errorf("ExternalID = %q, want the /music/ URL suffix", r.Sources[0].ExternalID)
		}
	})

	t.Run("mbid ref uses mbid param", func(t *testing.T) {
		if _, err := adapter.GetArtistTopTracks(context.Background(), domain.ProviderLastFM, lastfmTestMBID); err != nil {
			t.Fatalf("GetArtistTopTracks: %v", err)
		}
		if !strings.Contains(gotQuery, "mbid="+lastfmTestMBID) {
			t.Errorf("query = %q, want mbid= for an MBID-shaped ref (identity-safe path)", gotQuery)
		}
	})
}

func TestLastFmAdapter_GetArtistAlbums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"topalbums": {"album": [
			{
				"name": "After Hours",
				"playcount": 41000000,
				"mbid": "` + lastfmTestMBID + `",
				"url": "https://www.last.fm/music/The+Weeknd/After+Hours",
				"artist": {"name": "The Weeknd"},
				"image": [{"#text": "https://img/xl.png", "size": "extralarge"}]
			},
			{"name": "(null)", "url": "https://www.last.fm/music/The+Weeknd/x"},
			{"name": "", "url": "https://www.last.fm/music/The+Weeknd/y"}
		]}}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-key")
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderLastFM, "The Weeknd")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 ((null)/empty names dropped)", len(results))
	}
	r := results[0]
	if r.Kind != domain.ResultKindAlbum || r.Title != "After Hours" || r.Subtitle != "The Weeknd" {
		t.Errorf("result = %+v", r)
	}
	if r.MBID != lastfmTestMBID {
		t.Errorf("MBID = %q, want carried for the identifier merge tier", r.MBID)
	}
	if r.Extras["playcount"] != int64(41000000) {
		t.Errorf("playcount = %v, want int64", r.Extras["playcount"])
	}
}

func TestLastFmAdapter_GetArtistAlbums_httpErrorPropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-key")
	if _, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderLastFM, "X"); err == nil {
		t.Fatal("expected an error on HTTP 500")
	}
}

func TestLastFmAdapter_FetchCharts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("method") {
		case "chart.gettopartists":
			_, _ = w.Write([]byte(`{"artists": {"artist": [{"name": "The Weeknd", "listeners": "3,500,000"}]}}`))
		case "chart.gettoptracks":
			_, _ = w.Write([]byte(`{"tracks": {"track": [{"name": "Blinding Lights", "listeners": "2000000"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-key")
	entries, err := adapter.FetchCharts(context.Background(), 10)
	if err != nil {
		t.Fatalf("FetchCharts: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Kind != "artist" || entries[0].Popularity != 3500000 {
		t.Errorf("artist entry = %+v, want listeners parsed through separators", entries[0])
	}
	if entries[1].Kind != "track" || entries[1].Popularity != 2000000 {
		t.Errorf("track entry = %+v", entries[1])
	}
}

func TestLastFmAdapter_FetchCharts_failedMethodSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") == "chart.gettopartists" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tracks": {"track": [{"name": "Survivor", "listeners": "10"}]}}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-key")
	entries, err := adapter.FetchCharts(context.Background(), 10)
	if err != nil {
		t.Fatalf("FetchCharts must not fail when one chart method fails: %v", err)
	}
	if len(entries) != 1 || entries[0].Term != "Survivor" {
		t.Errorf("entries = %+v, want the surviving method's entry", entries)
	}
}

func TestLastfmExternalID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"artist url", "https://www.last.fm/music/The+Weeknd", "The+Weeknd"},
		{"track url", "https://www.last.fm/music/Katy+Perry/_/Small+Talk", "Katy+Perry/_/Small+Talk"},
		{"trailing slash trimmed", "https://www.last.fm/music/The+Weeknd/", "The+Weeknd"},
		{"no music prefix falls back to url", "https://example.com/thing", "https://example.com/thing"},
		{"empty", "", ""},
		{"bare prefix falls back to url", "https://www.last.fm/music/", "https://www.last.fm/music/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastfmExternalID(tt.in); got != tt.want {
				t.Errorf("lastfmExternalID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseListeners(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"1500000", 1500000},
		{"1,500,000", 1500000},
		{"", 0},
		{"n/a", 0},
	}
	for _, tt := range tests {
		if got := parseListeners(tt.in); got != tt.want {
			t.Errorf("parseListeners(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
