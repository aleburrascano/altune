package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

const testClientID = "abcdefghijklmnopqrstuvwxyz012345"

type soundCloudFake struct {
	srv   *httptest.Server
	home  int
	calls []string
}

func newSoundCloudFake(t *testing.T, bundle string, api http.HandlerFunc) *soundCloudFake {
	t.Helper()
	f := &soundCloudFake{}
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, _ *http.Request) {
		f.home++
		_, _ = w.Write([]byte(`<script src="` + f.srv.URL + `/assets/b.js"></script><script src="` + f.srv.URL + `/assets/a.js"></script><script src="` + f.srv.URL + `/assets/missing.js"></script>`))
	})
	mux.HandleFunc("/assets/b.js", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(bundle)) })
	mux.HandleFunc("/assets/a.js", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`no id here`)) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.Header.Get("User-Agent")+" "+r.URL.Path+"?"+r.URL.Query().Encode())
		api(w, r)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *soundCloudFake) source() liveSource {
	return newSoundCloudSource(f.srv.Client(), f.srv.URL+"/", f.srv.URL+"/api", nil)
}

func (f *soundCloudFake) wantCalls(t *testing.T, want ...string) {
	t.Helper()
	for i, call := range want {
		want[i] = soundCloudUserAgent + " /api" + strings.ReplaceAll(call, "SITE", url.QueryEscape(f.srv.URL))
	}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("got calls %q, want %q", f.calls, want)
	}
}

func okJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

func TestSoundCloudSendsTheAdapterRequestForEachEntity(t *testing.T) {
	cases := []struct{ entity, id, call string }{
		{"search_track", "nia archives", "/search/tracks?client_id=" + testClientID + "&limit=20&offset=0&q=nia+archives"},
		{"search_album", "luvleh", "/search/albums?client_id=" + testClientID + "&limit=20&q=luvleh"},
		{"search_user", "nia archives", "/search/users?client_id=" + testClientID + "&limit=20&q=nia+archives"},
		{"resolve", "nia-archives", "/resolve?client_id=" + testClientID + "&url=SITE%2Fnia-archives"},
		{"playlist", "9", "/playlists/9?client_id=" + testClientID},
		{"track", "7", "/tracks/7?client_id=" + testClientID},
		{"track_related", "7", "/tracks/7/related?client_id=" + testClientID + "&limit=20"},
		{"user_tracks", "42", "/users/42/tracks?client_id=" + testClientID + "&limit=50"},
		{"user_albums", "978352", "/users/978352/albums?client_id=" + testClientID + "&limit=50"},
	}
	for _, c := range cases {
		t.Run(c.entity, func(t *testing.T) {
			f := newSoundCloudFake(t, `client_id:"`+testClientID+`"`, okJSON(`{"id":1}`))
			if _, err := f.source().Resource(context.Background(), c.entity, c.id); err != nil {
				t.Fatal(err)
			}
			f.wantCalls(t, c.call)
		})
	}
}

func TestSoundCloudScrapesTheClientIDOnceSkippingBundlesWithoutIt(t *testing.T) {
	f := newSoundCloudFake(t, `x={client_id:"`+testClientID+`"}`, okJSON(`{"collection":[]}`))
	src := f.source()
	for range 2 {
		if _, err := src.Resource(context.Background(), "track", "7"); err != nil {
			t.Fatal(err)
		}
	}
	f.wantCalls(t, "/tracks/7?client_id="+testClientID, "/tracks/7?client_id="+testClientID)
	if f.home != 1 {
		t.Fatalf("home fetched %d times", f.home)
	}
}

func TestSoundCloudUserEntitiesResolveAPermalinkFirst(t *testing.T) {
	f := newSoundCloudFake(t, `client_id="`+testClientID+`"`, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/resolve" {
			_, _ = w.Write([]byte(`{"id":797158090,"kind":"user"}`))
			return
		}
		_, _ = w.Write([]byte(`{"collection":[]}`))
	})
	raw, err := f.source().Resource(context.Background(), "user_toptracks", "nia-archives")
	if err != nil || string(raw) != `{"collection":[]}` {
		t.Fatalf("got %s, %v", raw, err)
	}
	f.wantCalls(t, "/resolve?client_id="+testClientID+"&url=SITE%2Fnia-archives", "/users/797158090/toptracks?client_id="+testClientID+"&limit=50")
}

func TestSoundCloudFailsWhenResolveYieldsNoUser(t *testing.T) {
	f := newSoundCloudFake(t, `client_id="`+testClientID+`"`, okJSON(`{"kind":"track"}`))
	if _, err := f.source().Resource(context.Background(), "user_tracks", "someone"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSoundCloudFailsWhenNoBundleHasAClientID(t *testing.T) {
	f := newSoundCloudFake(t, `nothing`, okJSON(`{}`))
	if _, err := f.source().Resource(context.Background(), "track", "7"); err == nil {
		t.Fatal("want an error")
	}
	f.wantCalls(t)
}

func TestSoundCloudFailsOnAnErrorStatusWithoutTheClientID(t *testing.T) {
	f := newSoundCloudFake(t, `client_id="`+testClientID+`"`, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := f.source().Resource(context.Background(), "track", "7")
	if err == nil || !strings.Contains(err.Error(), "status 401") || strings.Contains(err.Error(), testClientID) {
		t.Fatalf("got %v", err)
	}
}
