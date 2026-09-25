package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLastFmAdapter_Search_Tracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"trackmatches": {
					"track": [{
						"name": "Small Talk",
						"artist": "Katy Perry",
						"mbid": "6c9d9e5f-25cc-4e3a-9d3a-3a4b7d2f1a01",
						"url": "https://www.last.fm/music/Katy+Perry/_/Small+Talk",
						"listeners": "1234567",
						"image": [
							{"#text": "https://lastfm.freetls.fastly.net/small.png", "size": "small"},
							{"#text": "https://lastfm.freetls.fastly.net/extralarge.png", "size": "extralarge"}
						]
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "small talk", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Small Talk" {
		t.Errorf("title: got %q, want %q", r.Title, "Small Talk")
	}
	if r.Subtitle != "Katy Perry" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Katy Perry")
	}
	if r.ImageURL != "https://lastfm.freetls.fastly.net/extralarge.png" {
		t.Errorf("imageURL: got %q, want extralarge image URL", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderLastFM {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderLastFM)
	}
	if r.Sources[0].ExternalID != "Katy+Perry/_/Small+Talk" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "Katy+Perry/_/Small+Talk")
	}
	if r.Sources[0].URL != "https://www.last.fm/music/Katy+Perry/_/Small+Talk" {
		t.Errorf("source URL: got %q, want last.fm track URL", r.Sources[0].URL)
	}
	if r.Extras["listeners"] != "1234567" {
		t.Errorf("extras.listeners: got %v, want %q", r.Extras["listeners"], "1234567")
	}
	if r.MBID != "6c9d9e5f-25cc-4e3a-9d3a-3a4b7d2f1a01" {
		t.Errorf("MBID: got %q, want the fixture mbid", r.MBID)
	}
}

func TestLastFmAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"artistmatches": {
					"artist": [{
						"name": "The Weeknd",
						"mbid": "c8b03190-306c-4120-bb0b-6f2ebfc06ea9",
						"url": "https://www.last.fm/music/The+Weeknd",
						"listeners": "9876543",
						"image": [
							{"#text": "https://lastfm.freetls.fastly.net/artist-small.png", "size": "small"},
							{"#text": "https://lastfm.freetls.fastly.net/artist-xl.png", "size": "extralarge"}
						]
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "the weeknd", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "The Weeknd" {
		t.Errorf("title: got %q, want %q", r.Title, "The Weeknd")
	}
	if r.ImageURL != "https://lastfm.freetls.fastly.net/artist-xl.png" {
		t.Errorf("imageURL: got %q, want extralarge artist image", r.ImageURL)
	}
	if r.Sources[0].ExternalID != "The+Weeknd" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "The+Weeknd")
	}
	if r.Extras["listeners"] != "9876543" {
		t.Errorf("extras.listeners: got %v, want %q", r.Extras["listeners"], "9876543")
	}
	if r.MBID != "c8b03190-306c-4120-bb0b-6f2ebfc06ea9" {
		t.Errorf("MBID: got %q, want the fixture mbid", r.MBID)
	}
}

func TestLastFmAdapter_Search_Albums_DoesNotStampReleaseMBID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"albummatches": {
					"album": [{
						"name": "OK Computer",
						"artist": "Radiohead",
						"mbid": "0b6b4ba0-d36f-47bd-b4ea-6a5b91842d29",
						"url": "https://www.last.fm/music/Radiohead/OK+Computer",
						"image": [
							{"#text": "https://lastfm.freetls.fastly.net/album-small.png", "size": "small"},
							{"#text": "https://lastfm.freetls.fastly.net/album-xl.png", "size": "extralarge"}
						]
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "ok computer", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindAlbum {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindAlbum)
	}
	if r.Title != "OK Computer" {
		t.Errorf("title: got %q, want %q", r.Title, "OK Computer")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.MBID != "" {
		t.Errorf("MBID: got %q, want empty (release-namespace mbid must not be stamped)", r.MBID)
	}
}

func TestLastFmAdapter_Search_Track_MissingListeners(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": {
				"trackmatches": {
					"track": [{
						"name": "Obscure Track",
						"artist": "Unknown",
						"url": "https://www.last.fm/music/Unknown/_/Obscure+Track",
						"image": []
					}]
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "obscure", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if _, ok := results[0].Extras["listeners"]; ok {
		t.Errorf("extras should not contain 'listeners' when API omits it")
	}
}

func TestLastFmAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewLastFmAdapter(newTestClient(server.URL), "test-api-key")
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected an error when all attempted kinds fail on HTTP 500, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

const lastfmTestMBID = "a74b1b7f-71a5-4011-9441-d0b5e4122711"

func TestLooksLikeMBID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{lastfmTestMBID, true},
		{strings.ToUpper(lastfmTestMBID), true},
		{"the weeknd", false},
		{"a74b1b7f-71a5-4011-9441-d0b5e412271", false},
		{"a74b1b7f_71a5_4011_9441_d0b5e4122711", false},
		{"g74b1b7f-71a5-4011-9441-d0b5e4122711", false},
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
