package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type seenRequest struct {
	method, uri, agent string
}

func recordingServer(t *testing.T, status int, body string) (*httptest.Server, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, seenRequest{method: r.Method, uri: r.URL.RequestURI(), agent: r.Header.Get("User-Agent")})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestITunesSendsTheAdapterRequests(t *testing.T) {
	cases := map[string]struct{ id, uri string }{
		"search_track":  {"Sober Feels", "/search?term=Sober+Feels&entity=song&country=US&limit=200"},
		"search_album":  {"Nia Archives Silence Is Loud", "/search?term=Nia+Archives+Silence+Is+Loud&entity=album&country=US&limit=200"},
		"search_artist": {"Subnation", "/search?term=Subnation&entity=musicArtist&country=US&limit=200"},
		"lookup_song":   {"1515978311", "/lookup?id=1515978311&entity=song&country=US&limit=50"},
		"lookup_album":  {"1515978300", "/lookup?id=1515978300&entity=album&country=US&limit=50"},
	}
	for entity, c := range cases {
		srv, seen := recordingServer(t, http.StatusOK, `{"resultCount":0,"results":[]}`)
		raw, err := newITunesSource(srv.Client(), srv.URL, newIntervalGate(0)).Resource(context.Background(), entity, c.id)
		if err != nil || !strings.Contains(string(raw), "resultCount") {
			t.Fatalf("%s: got %s, %v", entity, raw, err)
		}
		got := (*seen)[0]
		if got.method != http.MethodGet || got.uri != c.uri || got.agent != itunesUserAgent {
			t.Errorf("%s sent %+v, want %s", entity, got, c.uri)
		}
	}
	if len(cases) != len(itunesEndpoints) {
		t.Fatalf("%d cases for %d endpoints", len(cases), len(itunesEndpoints))
	}
}

func TestITunesPacesItsRequests(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusOK, `{"results":[]}`)
	src := newITunesSource(srv.Client(), srv.URL, newIntervalGate(time.Hour))
	if _, err := src.Resource(context.Background(), "lookup_song", "1"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := src.Resource(ctx, "lookup_album", "2"); err == nil {
		t.Fatal("want the second request to wait out the gap and hit the deadline")
	}
}

func TestITunesFailsOnAnErrorStatus(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusForbidden, "")
	if _, err := newITunesSource(srv.Client(), srv.URL, newIntervalGate(0)).Resource(context.Background(), "search_track", "x"); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("got %v", err)
	}
}
