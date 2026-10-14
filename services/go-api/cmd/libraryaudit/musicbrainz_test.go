package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

const recordingJSON = `{"recordings":[{"id":"mbid-1","title":"Valentina","length":236500,
"artist-credit":[{"name":"Daniel Caesar","artist":{"name":"Daniel Caesar"}}],"isrcs":["USX0000001"]}]}`

func musicBrainzFor(t *testing.T, handler http.HandlerFunc) *musicBrainzClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := newMusicBrainzClient(server.Client(), server.URL, 1000)
	client.backoff = time.Millisecond
	return client
}

func TestSearchRecordings_SendsTheDocumentedRequestAndMapsTheRecording(t *testing.T) {
	var request *http.Request
	client := musicBrainzFor(t, func(w http.ResponseWriter, r *http.Request) {
		request = r
		_, _ = w.Write([]byte(recordingJSON))
	})
	got, err := client.SearchRecordings(context.Background(), "Daniel Caesar", `Valentina (feat. "X")`)
	if err != nil || len(got) != 1 {
		t.Fatalf("err=%v got=%+v", err, got)
	}
	query := request.URL.Query()
	if request.URL.Path != "/ws/2/recording" || query.Get("inc") != "isrcs" || query.Get("fmt") != "json" ||
		query.Get("query") != `recording:"Valentina" AND artist:"Daniel Caesar"` || request.Header.Get("User-Agent") != musicBrainzUserAgent {
		t.Fatalf("request %s UA %q", request.URL, request.Header.Get("User-Agent"))
	}
	if got[0].ID != "mbid-1" || got[0].DurationSeconds != 236.5 || !got[0].hasISRC("usx-0000001") || got[0].Artists[0] != "Daniel Caesar" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestRecordingsByISRC_QueriesTheISRCField(t *testing.T) {
	var query url.Values
	client := musicBrainzFor(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"recordings":[]}`))
	})
	if _, err := client.RecordingsByISRC(context.Background(), "usx0000001"); err != nil || query.Get("query") != `isrc:"USX0000001"` {
		t.Fatalf("err=%v query=%q", err, query.Get("query"))
	}
}

func TestMusicBrainzClient_RetriesAThrottleThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := musicBrainzFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(recordingJSON))
	})
	if got, err := client.RecordingsByISRC(context.Background(), "USX0000001"); err != nil || len(got) != 1 || calls.Load() != 3 {
		t.Fatalf("err=%v got=%+v calls=%d", err, got, calls.Load())
	}
}

func TestMusicBrainzClient_ForbiddenIsABlock(t *testing.T) {
	client := musicBrainzFor(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	if _, err := client.RecordingsByISRC(context.Background(), "USX0000001"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("got %v", err)
	}
}

func TestMusicBrainzClient_OtherStatusIsAnOrdinaryFailure(t *testing.T) {
	client := musicBrainzFor(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })
	if _, err := client.RecordingsByISRC(context.Background(), "USX0000001"); err == nil || errors.Is(err, errCatalogBlocked) {
		t.Fatalf("got %v", err)
	}
}

const isrcLookupJSON = `{"recordings":[{"id":"rec-1","title":"Valentina","artist-credit":[{"name":"Daniel Caesar","joinphrase":"","artist":{"name":"Daniel Caesar"}}],
"releases":[{"id":"rel-1","title":"Freudian","date":"2017-08-25","artist-credit":[{"name":"Daniel Caesar","joinphrase":" & "},{"name":"Kali Uchis","joinphrase":""}]}]}]}`

func TestRecordingsWithReleases_SearchesTheISRCBecauseTheISRCEndpointOmitsReleases(t *testing.T) {
	var request *http.Request
	client := musicBrainzFor(t, func(w http.ResponseWriter, r *http.Request) {
		request = r
		_, _ = w.Write([]byte(isrcLookupJSON))
	})
	got, err := client.RecordingsWithReleases(context.Background(), "usx-0000001")
	if err != nil || len(got) != 1 || len(got[0].Releases) != 1 {
		t.Fatalf("err=%v got=%+v", err, got)
	}
	if request.URL.Path != "/ws/2/recording" || request.URL.Query().Get("query") != `isrc:"USX0000001"` || request.URL.Query().Get("fmt") != "json" {
		t.Fatalf("request %s", request.URL)
	}
	if want := (release{ID: "rel-1", Title: "Freudian", Date: "2017-08-25", Artist: "Daniel Caesar & Kali Uchis"}); got[0].Releases[0] != want {
		t.Fatalf("got %+v", got[0].Releases[0])
	}
}

func TestRecordingsWithReleases_AnISRCMusicBrainzDoesNotKnowIsNoRecordings(t *testing.T) {
	client := musicBrainzFor(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"recordings":[]}`)) })
	if got, err := client.RecordingsWithReleases(context.Background(), "USX0000001"); err != nil || len(got) != 0 {
		t.Fatalf("err=%v got=%+v", err, got)
	}
}

func TestMusicBrainzClient_ThrottleOnOneLookupIsAnOrdinaryFailureUntilItRepeats(t *testing.T) {
	var calls atomic.Int32
	client := musicBrainzFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	for lookup := 1; lookup < musicBrainzMaxThrottledInARow; lookup++ {
		if _, err := client.RecordingsByISRC(context.Background(), "USX0000001"); err == nil || errors.Is(err, errCatalogBlocked) {
			t.Fatalf("lookup %d: got %v", lookup, err)
		}
	}
	if _, err := client.RecordingsByISRC(context.Background(), "USX0000001"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("after %d lookups: got %v", musicBrainzMaxThrottledInARow, err)
	}
	if calls.Load() != musicBrainzMaxThrottledInARow*musicBrainzMaxAttempts {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestMusicBrainzClient_ASuccessResetsTheThrottleStreak(t *testing.T) {
	var throttle atomic.Bool
	throttle.Store(true)
	client := musicBrainzFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if throttle.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(recordingJSON))
	})
	for range musicBrainzMaxThrottledInARow - 1 {
		_, _ = client.RecordingsByISRC(context.Background(), "USX0000001")
	}
	throttle.Store(false)
	if _, err := client.RecordingsByISRC(context.Background(), "USX0000001"); err != nil {
		t.Fatal(err)
	}
	throttle.Store(true)
	if _, err := client.RecordingsByISRC(context.Background(), "USX0000001"); err == nil || errors.Is(err, errCatalogBlocked) {
		t.Fatalf("streak was not reset: %v", err)
	}
}
