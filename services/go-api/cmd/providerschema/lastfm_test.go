package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func lastfmServer(t *testing.T, body string, status int) (*httptest.Server, *url.Values) {
	t.Helper()
	got := &url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/2.0/" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		*got = r.URL.Query()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestLastfmSendsTheAdapterQueryForEachEntity(t *testing.T) {
	cases := []struct {
		entity, id string
		want       map[string]string
	}{
		{"search_track", "Baianá", map[string]string{"method": "track.search", "track": "Baianá", "limit": "15"}},
		{"search_album", "Luvleh Mix (DJ Mix)", map[string]string{"method": "album.search", "album": "Luvleh Mix (DJ Mix)", "limit": "15"}},
		{"search_artist", "Nia Archives", map[string]string{"method": "artist.search", "artist": "Nia Archives"}},
		{"chart_top_artists", "50", map[string]string{"method": "chart.gettopartists", "limit": "50"}},
		{"chart_top_tracks", "50", map[string]string{"method": "chart.gettoptracks", "limit": "50"}},
		{"artist_top_tracks", "Nia Archives", map[string]string{"method": "artist.gettoptracks", "artist": "Nia Archives", "limit": "10", "autocorrect": "1"}},
		{"artist_top_tracks", "09d6b57e-9cd9-4a74-b7cb-842b8443a1cd", map[string]string{"mbid": "09d6b57e-9cd9-4a74-b7cb-842b8443a1cd", "artist": ""}},
		{"artist_top_albums", "Cardi B", map[string]string{"method": "artist.gettopalbums", "artist": "Cardi B", "limit": "50"}},
		{"artist_info", "Subnation", map[string]string{"method": "artist.getinfo", "artist": "Subnation", "autocorrect": "1"}},
		{"track_info", "Cardi B|WAP", map[string]string{"method": "track.getinfo", "artist": "Cardi B", "track": "WAP"}},
		{"album_info", "Nia Archives|Silence Is Loud", map[string]string{"method": "album.getinfo", "artist": "Nia Archives", "album": "Silence Is Loud"}},
	}
	for _, c := range cases {
		srv, got := lastfmServer(t, `{}`, http.StatusOK)
		if _, err := newLastfmSource(srv.Client(), srv.URL+"/2.0/", "k").Resource(context.Background(), c.entity, c.id); err != nil {
			t.Fatalf("%s: %v", c.entity, err)
		}
		c.want["api_key"], c.want["format"] = "k", "json"
		for name, value := range c.want {
			if got.Get(name) != value {
				t.Errorf("%s %s: %s = %q, want %q", c.entity, c.id, name, got.Get(name), value)
			}
		}
	}
}

func TestLastfmFailsOnAnErrorEnvelopeWithoutTheKey(t *testing.T) {
	srv, _ := lastfmServer(t, `{"error":6,"message":"Track not found"}`, http.StatusOK)
	_, err := newLastfmSource(srv.Client(), srv.URL+"/2.0/", "secret").Resource(context.Background(), "track_info", "a|b")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestLastfmFailsOnAnErrorStatusAndABadFixture(t *testing.T) {
	srv, _ := lastfmServer(t, ``, http.StatusForbidden)
	src := newLastfmSource(srv.Client(), srv.URL+"/2.0/", "k")
	if _, err := src.Resource(context.Background(), "search_track", "x"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
	if _, err := src.Resource(context.Background(), "album_info", "no separator"); err == nil {
		t.Fatal("want an error for a fixture that is not artist|title")
	}
}

func TestLastfmProviderNeedsTheKey(t *testing.T) {
	t.Setenv("LASTFM_API_KEY", "")
	if _, err := lastfmProvider().source(http.DefaultClient); err == nil {
		t.Fatal("want an error")
	}
}
