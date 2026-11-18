package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func theAudioDBServer(t *testing.T, status int) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Clone(context.Background()))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestTheAudioDBSendsTheAdapterRequestsWithTheKeyInThePath(t *testing.T) {
	srv, got := theAudioDBServer(t, http.StatusOK)
	src := newTheAudioDBSource(srv.Client(), srv.URL+"/api/v1/json/", "k")
	cases := []struct{ entity, id, uri string }{
		{"search_artist", "Nia Archives", "/api/v1/json/k/search.php?s=Nia+Archives"},
		{"search_album", "Cardi B|Invasion of Privacy", "/api/v1/json/k/searchalbum.php?s=Cardi+B&a=Invasion+of+Privacy"},
		{"artist_mb", "09d6b57e-9cd9-4a74-b7cb-842b8443a1cd", "/api/v1/json/k/artist-mb.php?i=09d6b57e-9cd9-4a74-b7cb-842b8443a1cd"},
	}
	for _, c := range cases {
		if _, err := src.Resource(context.Background(), c.entity, c.id); err != nil {
			t.Fatalf("%s: %v", c.entity, err)
		}
		r := (*got)[len(*got)-1]
		if r.Method != http.MethodGet || r.URL.RequestURI() != c.uri {
			t.Errorf("%s: got %s %s, want %s", c.entity, r.Method, r.URL.RequestURI(), c.uri)
		}
	}
}

func TestTheAudioDBRejectsABadAlbumFixtureAndAnErrorStatus(t *testing.T) {
	srv, got := theAudioDBServer(t, http.StatusNotFound)
	src := newTheAudioDBSource(srv.Client(), srv.URL+"/api/v1/json/", "k")
	if _, err := src.Resource(context.Background(), "search_album", "no separator"); err == nil || len(*got) != 0 {
		t.Fatalf("got %v after %d requests", err, len(*got))
	}
	if _, err := src.Resource(context.Background(), "search_artist", "x"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("got %v", err)
	}
}

func TestTheAudioDBProviderFallsBackToThePublicTestKey(t *testing.T) {
	t.Setenv("THEAUDIODB_API_KEY", "")
	src, err := theAudioDBProvider().source(http.DefaultClient)
	if err != nil || src == nil {
		t.Fatalf("got %v, %v", src, err)
	}
}
