package main

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discogsServer(t *testing.T, status int) (*httptest.Server, *[]*http.Request) {
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

func TestDiscogsSendsTheAdapterRequests(t *testing.T) {
	srv, got := discogsServer(t, http.StatusOK)
	src := newDiscogsSource(srv.Client(), srv.URL, "tok", newIntervalGate(clock.Now, 0))
	want := map[string]string{
		"search_artist":   "/database/search?type=artist&q=Nia+Archives&per_page=5",
		"artist":          "/artists/9824716",
		"artist_releases": "/artists/9824716/releases?sort=year&sort_order=desc&per_page=100",
	}
	ids := map[string]string{"search_artist": "Nia Archives", "artist": "9824716", "artist_releases": "9824716|100"}
	for entity, uri := range want {
		if _, err := src.Resource(context.Background(), entity, ids[entity]); err != nil {
			t.Fatalf("%s: %v", entity, err)
		}
		r := (*got)[len(*got)-1]
		if r.Method != http.MethodGet || r.URL.RequestURI() != uri {
			t.Errorf("%s: got %s %s, want %s", entity, r.Method, r.URL.RequestURI(), uri)
		}
		if r.Header.Get("Authorization") != "Discogs token=tok" || r.Header.Get("User-Agent") != discogsUserAgent {
			t.Errorf("%s: headers %v", entity, r.Header)
		}
	}
	if len(want) != len(discogsEndpoints) {
		t.Fatalf("%d cases for %d endpoints", len(want), len(discogsEndpoints))
	}
}

func TestDiscogsRejectsBadFixturesBeforeSending(t *testing.T) {
	srv, got := discogsServer(t, http.StatusOK)
	src := newDiscogsSource(srv.Client(), srv.URL, "tok", newIntervalGate(clock.Now, 0))
	for entity, id := range map[string]string{"artist": "nia", "artist_releases": "9824716", "artist_releases ": "x|1"} {
		if _, err := src.Resource(context.Background(), strings.TrimSpace(entity), id); err == nil {
			t.Errorf("%s %q: want an error", entity, id)
		}
	}
	if len(*got) != 0 {
		t.Fatalf("sent %d requests", len(*got))
	}
}

func TestDiscogsFailsOnAnErrorStatusWithoutTheToken(t *testing.T) {
	srv, _ := discogsServer(t, http.StatusTooManyRequests)
	_, err := newDiscogsSource(srv.Client(), srv.URL, "secret", newIntervalGate(clock.Now, 0)).Resource(context.Background(), "artist", "1")
	if err == nil || !strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestDiscogsPacesItsRequests(t *testing.T) {
	srv, got := discogsServer(t, http.StatusOK)
	src := newDiscogsSource(srv.Client(), srv.URL, "tok", newIntervalGate(clock.Now, 50*time.Millisecond))
	start := time.Now()
	for range 3 {
		if _, err := src.Resource(context.Background(), "artist", "1"); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || len(*got) != 3 {
		t.Fatalf("%d requests took %v", len(*got), elapsed)
	}
}

func TestDiscogsProviderNeedsTheToken(t *testing.T) {
	t.Setenv("DISCOGS_TOKEN", "")
	if _, err := discogsProvider().source(http.DefaultClient); err == nil {
		t.Fatal("want an error")
	}
}
