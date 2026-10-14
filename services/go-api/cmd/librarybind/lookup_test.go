package main

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const deezerTrackJSON = `{"id":3135556,"title":"Celebration","isrc":"USPR18000001","duration":298,
	"artist":{"name":"Kool & The Gang"},"contributors":[{"name":"Kool & The Gang"}],"album":{"title":"Celebrate!"}}`

func fastDeezer(srv *httptest.Server) deezerLookup {
	lookup := newDeezerLookup(srv.Client(), srv.URL, 1000)
	lookup.client.backoff = time.Millisecond
	return lookup
}

func TestDeezerLookup_ByISRC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/track/isrc:USPR18000001" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(deezerTrackJSON))
	}))
	defer srv.Close()
	got, err := fastDeezer(srv).ByISRC(t.Context(), "us-pr1-8000001")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].ID != "3135556" || got[0].LengthSeconds != 298 || got[0].Title != "Celebration" || got[0].Album != "Celebrate!" || got[0].ISRC != "USPR18000001" {
		t.Errorf("candidate = %+v", got[0])
	}
}

func TestDeezerLookup_NoDataIsNoCandidate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"type":"DataException","code":800}}`))
	}))
	defer srv.Close()
	got, err := fastDeezer(srv).ByISRC(t.Context(), "USPR18000001")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDeezerLookup_AbortsOnForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := fastDeezer(srv).ByISRC(t.Context(), "USPR18000001"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v, want blocked", err)
	}
}

func TestDeezerLookup_RetriesQuotaThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"error":{"code":4}}`))
			return
		}
		_, _ = w.Write([]byte(deezerTrackJSON))
	}))
	defer srv.Close()
	got, err := fastDeezer(srv).ByISRC(t.Context(), "USPR18000001")
	if err != nil || len(got) != 1 || calls.Load() != 2 {
		t.Fatalf("got %+v, %v after %d calls", got, err, calls.Load())
	}
}

func TestDeezerLookup_GivesUpAsBlockedWhenAlwaysThrottled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if _, err := fastDeezer(srv).ByISRC(t.Context(), "USPR18000001"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v, want blocked", err)
	}
}

func TestMusicBrainzLookup_ByISRCCollectsCreditsAndAliases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != `isrc:"GBAYE8400001"` {
			t.Errorf("query = %q", r.URL.Query().Get("query"))
		}
		_, _ = w.Write([]byte(`{"recordings":[{"id":"mb-1","title":"Careless Whisper","length":298000,"isrcs":["GBAYE8400001","GBAYE8400002"],
			"artist-credit":[{"name":"Wham!","artist":{"name":"George Michael","aliases":[{"name":"Georgios Panayiotou"}]}}]}]}`))
	}))
	defer srv.Close()
	lookup := newMusicBrainzLookup(srv.Client(), srv.URL, 1000)
	got, err := lookup.ByISRC(t.Context(), "GBAYE8400001")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].ID != "mb-1" || got[0].LengthSeconds != 298 || got[0].ISRC != "GBAYE8400001" {
		t.Errorf("candidate = %+v", got[0])
	}
	if !artistMatches("Wham!", got[0].Artists) || !artistMatches("George Michael", got[0].Artists) || !artistMatches("Georgios Panayiotou", got[0].Artists) {
		t.Errorf("artists = %v", got[0].Artists)
	}
}

func TestMusicBrainzLookup_AbortsOnForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := newMusicBrainzLookup(srv.Client(), srv.URL, 1000).ByISRC(t.Context(), "GBAYE8400001"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v, want blocked", err)
	}
}

type fakeAppleSongs struct {
	songs []domain.SearchResult
	err   error
}

func (f fakeAppleSongs) SongsByISRC(context.Context, string) ([]domain.SearchResult, error) {
	return f.songs, f.err
}

func TestAppleLookup_ByISRCMapsSongs(t *testing.T) {
	song := domain.SearchResult{
		Title: "Celebration", Subtitle: "Kool & The Gang", Album: "Celebrate!", Duration: 298, ISRC: "USPR18000001",
		Sources: []domain.SourceRef{{Provider: domain.ProviderAppleMusic, ExternalID: "a1"}},
	}
	got, err := newAppleLookup(fakeAppleSongs{songs: []domain.SearchResult{song}}, 1000).ByISRC(t.Context(), "USPR18000001")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].ID != "a1" || got[0].LengthSeconds != 298 || got[0].Artists[0] != "Kool & The Gang" || got[0].Album != "Celebrate!" {
		t.Errorf("candidate = %+v", got[0])
	}
}

func TestAppleLookup_AbortsWhenBlocked(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		lookup := newAppleLookup(fakeAppleSongs{err: providerhttp.StatusError{Status: status}}, 1000)
		if _, err := lookup.ByISRC(t.Context(), "USPR18000001"); !errors.Is(err, errCatalogBlocked) {
			t.Errorf("status %d: err = %v, want blocked", status, err)
		}
	}
}

func TestAppleLookup_OtherErrorsStayPerTrack(t *testing.T) {
	lookup := newAppleLookup(fakeAppleSongs{err: providerhttp.StatusError{Status: http.StatusInternalServerError}}, 1000)
	_, err := lookup.ByISRC(t.Context(), "USPR18000001")
	if err == nil || errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v", err)
	}
}
