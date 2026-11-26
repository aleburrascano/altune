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

type musicBrainzSeen struct {
	method, uri, agent, accept string
}

func musicBrainzServer(t *testing.T, statuses ...int) (*httptest.Server, *[]musicBrainzSeen) {
	t.Helper()
	var seen []musicBrainzSeen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, musicBrainzSeen{method: r.Method, uri: r.URL.RequestURI(), agent: r.Header.Get("User-Agent"), accept: r.Header.Get("Accept")})
		status := http.StatusOK
		if len(seen) <= len(statuses) {
			status = statuses[len(seen)-1]
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func testMusicBrainzSource(srv *httptest.Server) liveSource {
	return newMusicBrainzSource(srv.Client(), srv.URL, newIntervalGate(clock.Now, 0), time.Millisecond)
}

func TestMusicBrainzSendsTheAdapterRequests(t *testing.T) {
	cases := map[string]struct{ id, uri string }{
		"search_recording":      {`artist:"Nia Archives" AND recording:"Sober Feels"`, "/recording/?query=artist%3A%22Nia+Archives%22+AND+recording%3A%22Sober+Feels%22&fmt=json&limit=15&inc=isrcs"},
		"search_artist":         {"Nia Archives", "/artist/?query=Nia+Archives&fmt=json&limit=15"},
		"search_release_group":  {"Silence Is Loud", "/release-group/?query=Silence+Is+Loud&fmt=json&limit=15"},
		"isrc":                  {"QZHN42091541", "/isrc/QZHN42091541?fmt=json"},
		"recording_detail":      {"r1", "/recording/r1?inc=isrcs+artist-credits+releases&fmt=json"},
		"recording":             {"r1", "/recording/r1?inc=isrcs+artist-credits&fmt=json"},
		"recording_works":       {"r1", "/recording/r1?inc=work-rels+artist-credits&fmt=json"},
		"work":                  {"w1", "/work/w1?inc=recording-rels+artist-credits&fmt=json"},
		"artist":                {"a1", "/artist/a1?inc=genres+ratings+url-rels&fmt=json"},
		"release_group":         {"g1", "/release-group/g1?inc=genres+ratings&fmt=json"},
		"browse_release_groups": {"a1", "/release-group?artist=a1&type=album%7Cep%7Csingle&fmt=json&limit=100&offset=0"},
	}
	for entity, c := range cases {
		srv, seen := musicBrainzServer(t)
		if _, err := testMusicBrainzSource(srv).Resource(context.Background(), entity, c.id); err != nil {
			t.Fatalf("%s: %v", entity, err)
		}
		got := (*seen)[0]
		if got.method != http.MethodGet || got.uri != c.uri || got.agent != musicBrainzUserAgent || got.accept != "application/json" {
			t.Errorf("%s sent %+v, want uri %s", entity, got, c.uri)
		}
	}
	if len(cases) != len(musicBrainzEndpoints) {
		t.Fatalf("%d cases for %d endpoints", len(cases), len(musicBrainzEndpoints))
	}
}

func TestMusicBrainzRetriesWhileTheServerIsBusy(t *testing.T) {
	srv, seen := musicBrainzServer(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable)
	raw, err := testMusicBrainzSource(srv).Resource(context.Background(), "recording", "r1")
	if err != nil || string(raw) != `{"id":"x"}` || len(*seen) != 3 {
		t.Fatalf("got %s, %v after %d calls", raw, err, len(*seen))
	}
}

func TestMusicBrainzGivesUpWhenTheServerStaysBusy(t *testing.T) {
	busy := make([]int, musicBrainzRetries+1)
	for i := range busy {
		busy[i] = http.StatusServiceUnavailable
	}
	srv, seen := musicBrainzServer(t, busy...)
	_, err := testMusicBrainzSource(srv).Resource(context.Background(), "artist", "a1")
	if err == nil || !strings.Contains(err.Error(), "status 503") || len(*seen) != musicBrainzRetries+1 {
		t.Fatalf("got %v after %d calls", err, len(*seen))
	}
}

func TestMusicBrainzFailsAtOnceOnOtherErrors(t *testing.T) {
	srv, seen := musicBrainzServer(t, http.StatusNotFound)
	_, err := testMusicBrainzSource(srv).Resource(context.Background(), "work", "w1")
	if err == nil || !strings.Contains(err.Error(), "status 404") || len(*seen) != 1 {
		t.Fatalf("got %v after %d calls", err, len(*seen))
	}
}

func TestMusicBrainzStopsRetryingWhenTheContextEnds(t *testing.T) {
	srv, _ := musicBrainzServer(t, http.StatusServiceUnavailable)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := newMusicBrainzSource(srv.Client(), srv.URL, newIntervalGate(clock.Now, 0), time.Hour).Resource(ctx, "artist", "a1"); err == nil {
		t.Fatal("want the context error")
	}
}
