package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func geniusServer(t *testing.T, status int) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Clone(r.Context()))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestGeniusSendsTheAdapterRequests(t *testing.T) {
	srv, got := geniusServer(t, http.StatusOK)
	src := newGeniusSource(srv.Client(), srv.URL, "tok")
	cases := []struct{ entity, id, uri string }{
		{"search", "Nia Archives songs", "/search?q=Nia+Archives+songs"},
		{"search_songs", "Cardi B WAP", "/search?q=Cardi+B+WAP"},
		{"artist_songs", "2696344|2", "/artists/2696344/songs?sort=popularity&per_page=50&page=2"},
		{"song", "8427586", "/songs/8427586"},
	}
	for _, c := range cases {
		if _, err := src.Resource(context.Background(), c.entity, c.id); err != nil {
			t.Fatalf("%s: %v", c.entity, err)
		}
		r := (*got)[len(*got)-1]
		if r.Method != http.MethodGet || r.URL.RequestURI() != c.uri || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s: got %s %s auth %q", c.entity, r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"))
		}
	}
	if len(cases) != len(geniusEndpoints) {
		t.Fatalf("%d cases for %d endpoints", len(cases), len(geniusEndpoints))
	}
}

func TestGeniusRejectsBadFixturesBeforeSending(t *testing.T) {
	srv, got := geniusServer(t, http.StatusOK)
	src := newGeniusSource(srv.Client(), srv.URL, "tok")
	bad := []struct{ entity, id string }{{"song", "baiana"}, {"artist_songs", "2696344"}, {"artist_songs", "2696344|0"}, {"artist_songs", "x|1"}}
	for _, c := range bad {
		if _, err := src.Resource(context.Background(), c.entity, c.id); err == nil {
			t.Errorf("%s %q: want an error", c.entity, c.id)
		}
	}
	if len(*got) != 0 {
		t.Fatalf("sent %d requests", len(*got))
	}
}

func TestGeniusFailsOnAnErrorStatusWithoutTheToken(t *testing.T) {
	srv, _ := geniusServer(t, http.StatusUnauthorized)
	_, err := newGeniusSource(srv.Client(), srv.URL, "secret").Resource(context.Background(), "song", "1")
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestGeniusProviderNeedsTheToken(t *testing.T) {
	t.Setenv("GENIUS_ACCESS_TOKEN", "")
	if _, err := geniusProvider().source(http.DefaultClient); err == nil {
		t.Fatal("want an error")
	}
}
