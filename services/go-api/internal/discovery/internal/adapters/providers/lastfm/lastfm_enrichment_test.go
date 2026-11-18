package lastfm_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const lastfmArtistInfoJSON = `{
  "artist": {
    "name": "Kendrick Lamar",
    "mbid": "381086ea-f511-4aba-bdf9-71c753dc5077",
    "stats": { "listeners": "5172275", "playcount": "1050884806" },
    "similar": { "artist": [
      { "name": "Baby Keem" },
      { "name": "Jay Rock" },
      { "name": "JID" }
    ] },
    "tags": { "tag": [
      { "name": "Hip-Hop" },
      { "name": "rap" },
      { "name": "west coast" }
    ] },
    "bio": { "summary": "Kendrick Lamar Duckworth is an American rapper. <a href=\"https://www.last.fm/music/Kendrick+Lamar\">Read more on Last.fm</a>" }
  }
}`

const lastfmTrackInfoJSON = `{
  "track": {
    "name": "HUMBLE.",
    "mbid": "",
    "listeners": "2303705",
    "playcount": "31810978",
    "duration": "199000",
    "album": { "title": "DAMN." },
    "toptags": { "tag": [
      { "name": "rap" },
      { "name": "trap" },
      { "name": "Hip-Hop" }
    ] },
    "wiki": { "summary": "\"HUMBLE.\" is a song by Kendrick Lamar. <a href=\"https://www.last.fm/\">Read more on Last.fm</a>" }
  }
}`

const lastfmAlbumInfoJSON = `{
  "album": {
    "name": "DAMN.",
    "mbid": "503c4a0f-97b9-4d6b-9a27-52a7f6b21cc9",
    "listeners": "3185965",
    "playcount": "207417230",
    "tags": { "tag": [ { "name": "rap" }, { "name": "hip-hop" } ] },
    "wiki": { "summary": "DAMN. is the fourth studio album by Kendrick Lamar." }
  }
}`

func lastfmEnrichmentServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("method") {
		case "artist.getinfo":
			_, _ = w.Write([]byte(lastfmArtistInfoJSON))
		case "track.getinfo":
			_, _ = w.Write([]byte(lastfmTrackInfoJSON))
		case "album.getinfo":
			_, _ = w.Write([]byte(lastfmAlbumInfoJSON))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLastFmAdapter_Lookup_Artist(t *testing.T) {
	server := lastfmEnrichmentServer(t)
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	e, err := adapter.Lookup(context.Background(), discovery.ResultKindArtist, "Kendrick Lamar", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.MBID != "381086ea-f511-4aba-bdf9-71c753dc5077" {
		t.Errorf("mbid: got %q", e.MBID)
	}
	if e.Listeners != 5172275 || e.Playcount != 1050884806 {
		t.Errorf("popularity: got listeners=%d playcount=%d", e.Listeners, e.Playcount)
	}
	if len(e.Tags) != 3 || e.Tags[0] != "Hip-Hop" {
		t.Errorf("tags: got %v", e.Tags)
	}
	if len(e.Similar) != 3 || e.Similar[0] != "Baby Keem" {
		t.Errorf("similar: got %v", e.Similar)
	}
	if e.Bio != "Kendrick Lamar Duckworth is an American rapper." {
		t.Errorf("bio not cleaned: got %q", e.Bio)
	}
}

func TestLastFmAdapter_Lookup_Track(t *testing.T) {
	server := lastfmEnrichmentServer(t)
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	e, err := adapter.Lookup(context.Background(), discovery.ResultKindTrack, "Kendrick Lamar", "HUMBLE.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Duration != 199 {
		t.Errorf("duration: got %d, want 199 (ms→s)", e.Duration)
	}
	if e.Album != "DAMN." {
		t.Errorf("album: got %q", e.Album)
	}
	if e.Listeners != 2303705 {
		t.Errorf("listeners: got %d", e.Listeners)
	}
	if len(e.Tags) != 3 || e.Tags[1] != "trap" {
		t.Errorf("tags: got %v", e.Tags)
	}
	if len(e.Similar) != 0 {
		t.Errorf("similar should be empty for track: got %v", e.Similar)
	}
}

func TestLastFmAdapter_Lookup_Album(t *testing.T) {
	server := lastfmEnrichmentServer(t)
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	e, err := adapter.Lookup(context.Background(), discovery.ResultKindAlbum, "Kendrick Lamar", "DAMN.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.MBID != "503c4a0f-97b9-4d6b-9a27-52a7f6b21cc9" {
		t.Errorf("mbid: got %q", e.MBID)
	}
	if e.Playcount != 207417230 {
		t.Errorf("playcount: got %d", e.Playcount)
	}
	if len(e.Tags) != 2 {
		t.Errorf("tags: got %v", e.Tags)
	}
}

func TestLastFmAdapter_Lookup_HTTPErrorDegrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	_, err := adapter.Lookup(context.Background(), discovery.ResultKindArtist, "Kendrick Lamar", "")
	if err == nil {
		t.Fatal("expected an error on HTTP 500 so the service can degrade to empty")
	}
}

func TestLastFmAdapter_Lookup_InBandError6IsCleanMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":6,"message":"Artist not found"}`))
	}))
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	e, err := adapter.Lookup(context.Background(), discovery.ResultKindArtist, "No Such Artist", "")
	if err != nil {
		t.Fatalf("error 6 must be a clean miss (nil error), got %v", err)
	}
	if !e.IsZero() {
		t.Errorf("error 6 must yield a zero enrichment, got %+v", e)
	}
}

func TestLastFmAdapter_Lookup_InBandError6On4xxIsCleanMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":6,"message":"Track not found"}`))
	}))
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	e, err := adapter.Lookup(context.Background(), discovery.ResultKindTrack, "Nobody", "Nothing")
	if err != nil {
		t.Fatalf("4xx-delivered error 6 must be a clean miss (nil error), got %v", err)
	}
	if !e.IsZero() {
		t.Errorf("4xx-delivered error 6 must yield a zero enrichment, got %+v", e)
	}
}

func TestLastFmAdapter_Lookup_InBandErrorOtherIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":8,"message":"Operation failed - Something went wrong"}`))
	}))
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")

	_, err := adapter.Lookup(context.Background(), discovery.ResultKindArtist, "Kendrick Lamar", "")
	if err == nil {
		t.Fatal("error 8 must surface as a transient error, not a zero-struct miss")
	}
}

func lookupArtistFrom(t *testing.T, body string) (discovery.LastFmEnrichment, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	adapter := discovery.NewLastFmAdapter(providertest.NewClient(server.URL), "test-api-key")
	return adapter.Lookup(context.Background(), discovery.ResultKindArtist, "Kendrick Lamar", "")
}

func TestLastFmAdapter_Lookup_CleansTheBio(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips read-more anchor", `Foo bar. <a href="x">Read more on Last.fm</a>`, "Foo bar."},
		{"strips inline tags", `Foo <b>bar</b> baz`, "Foo bar baz"},
		{"unescapes entities", `Foo &amp; bar`, "Foo & bar"},
		{"empty stays empty", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			e, err := lookupArtistFrom(t, `{"artist":{"bio":{"summary":`+string(summary)+`}}}`)
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if e.Bio != tt.want {
				t.Errorf("bio from %q = %q, want %q", tt.in, e.Bio, tt.want)
			}
		})
	}
}

func TestLastFmAdapter_Lookup_TolerantOfEmptyTags(t *testing.T) {
	for _, body := range []string{`{"artist":{"tags":""}}`, `{"artist":{}}`} {
		e, err := lookupArtistFrom(t, body)
		if err != nil {
			t.Fatalf("Lookup(%s): %v", body, err)
		}
		if len(e.Tags) != 0 {
			t.Errorf("tags from %s = %v, want none", body, e.Tags)
		}
	}
}

func TestLastFmAdapter_Lookup_InBandErrorNamesCodeAndMessage(t *testing.T) {
	_, err := lookupArtistFrom(t, `{"error":29,"message":"Rate limit exceeded"}`)
	if err == nil || !strings.Contains(err.Error(), "29") || !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("Lookup error = %v, want code and message surfaced", err)
	}
}
