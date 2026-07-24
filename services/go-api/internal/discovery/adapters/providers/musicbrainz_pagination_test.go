package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestMusicBrainz_FetchReleaseGroups_laterPageErrorKeepsEarlierPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("offset") != "0" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// release-group-count says there is more, forcing a second page.
		_, _ = w.Write([]byte(`{
			"release-groups": [{"id": "rg-1", "title": "One", "primary-type": "Album"}],
			"release-group-count": 150
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("expected the partial set on a later-page failure, got error: %v", err)
	}
	if len(rgs) != 1 || rgs[0].Title != "One" {
		t.Fatalf("rgs = %+v, want the 1 page-1 release-group kept", rgs)
	}
	// A truncated set must NOT be memoized — the next call refetches.
	if _, ok := adapter.releaseMemo.get("mbid-1"); ok {
		t.Error("partial release-group set was memoized; a truncated discography would be reused for hours")
	}
}

func TestMusicBrainz_FetchReleaseGroups_singleflightCollapsesConcurrentFetches(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		_, _ = w.Write([]byte(`{
			"release-groups": [{"id": "rg-1", "title": "One", "primary-type": "Album"}],
			"release-group-count": 1
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-1")
			if err != nil || len(rgs) != 1 {
				t.Errorf("fetchReleaseGroups = (%v, %v), want 1 release-group", rgs, err)
			}
		}()
	}
	wg.Wait()
	if got := requests.Load(); got != 1 {
		t.Errorf("underlying MB requests = %d, want 1 (concurrent detail-opens must collapse)", got)
	}
}

func TestMBStructuredQuery_escapesEmbeddedQuotes(t *testing.T) {
	got := mbStructuredQuery(`The "Best" Band`, `Hello`, domain.ResultKindTrack)
	want := `artist:"The \"Best\" Band" AND recording:"Hello"`
	if got != want {
		t.Errorf("query = %q, want %q (embedded quotes backslash-escaped)", got, want)
	}
}
