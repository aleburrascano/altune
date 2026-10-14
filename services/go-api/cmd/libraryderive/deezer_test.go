package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const deezerTrackJSON = `{"id":3135556,"title":"Celebration","isrc":"USPR38007190","duration":298,"track_position":1,
	"album":{"id":80,"title":"Celebrate!"}}`

const deezerAlbumJSON = `{"id":80,"title":"Celebrate!","cover_xl":"https://cdn/xl.jpg","cover_big":"https://cdn/big.jpg",
	"genres":{"data":[{"name":"R&B"},{"name":"Pop"}]},"tracks":{"data":[{"id":3135556,"title":"Celebration"}]}}`

func fastDeezer(srv *httptest.Server) deezer {
	d := newDeezer(srv.Client(), srv.URL, 1000)
	d.client.backoff = time.Millisecond
	return d
}

func TestDeezer_ReadsATrackByIsrcWithItsAlbumAndPosition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/track/isrc:USPR38007190" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(deezerTrackJSON))
	}))
	defer srv.Close()
	got, err := fastDeezer(srv).TrackByISRC(t.Context(), "us-pr3-8007190")
	want := deezerTrack{ID: "3135556", ISRC: "USPR38007190", Title: "Celebration", Position: 1, DurationSeconds: 298, AlbumID: "80", AlbumTitle: "Celebrate!"}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDeezer_ReadsAnAlbumWithItsFirstGenreCoverAndTracks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(deezerAlbumJSON)) }))
	defer srv.Close()
	got, err := fastDeezer(srv).Album(t.Context(), "80")
	if err != nil || got.Genre != "R&B" || got.CoverURL != "https://cdn/xl.jpg" || len(got.Tracks) != 1 || got.Tracks[0].ID != "3135556" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDeezer_SearchesAlbumsByArtistAndTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/search/album" || r.URL.Query().Get("q") != `artist:"Kool & The Gang" album:"Celebrate!"` {
			t.Errorf("request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":[` + deezerAlbumJSON + `]}`))
	}))
	defer srv.Close()
	got, err := fastDeezer(srv).SearchAlbums(t.Context(), "Kool & The Gang", "Celebrate!")
	if err != nil || len(got) != 1 || got[0].ID != "80" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDeezer_NoDataIsUnknownAndForbiddenIsBlocked(t *testing.T) {
	body, status := `{"error":{"type":"DataException","code":800}}`, http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	d := fastDeezer(srv)
	if _, err := d.TrackByISRC(t.Context(), "USPR38007190"); !errors.Is(err, errCatalogUnknown) {
		t.Fatalf("err = %v", err)
	}
	status = http.StatusForbidden
	if _, err := d.TrackByID(t.Context(), "1"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeezer_RetriesTheQuotaErrorThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"error":{"type":"Exception","code":4}}`))
			return
		}
		_, _ = w.Write([]byte(deezerTrackJSON))
	}))
	defer srv.Close()
	if _, err := fastDeezer(srv).TrackByID(t.Context(), "3135556"); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDeezer_QuotaExhaustionIsBlockedAfterFourAttempts(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"error":{"type":"Exception","code":4}}`))
	}))
	defer srv.Close()
	if _, err := fastDeezer(srv).TrackByID(t.Context(), "1"); !errors.Is(err, errCatalogBlocked) || calls != 4 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDeezer_AnHTTP429IsRetriedThenBlocked(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if _, err := fastDeezer(srv).TrackByID(t.Context(), "1"); !errors.Is(err, errCatalogBlocked) || calls != 4 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDeezer_FiveServerErrorsInARowAbortAndASuccessResetsTheCount(t *testing.T) {
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(deezerTrackJSON))
		}
	}))
	defer srv.Close()
	d := fastDeezer(srv)
	for range 4 {
		if _, err := d.TrackByID(t.Context(), "1"); err == nil || errors.Is(err, errCatalogBlocked) {
			t.Fatalf("err = %v", err)
		}
	}
	status = http.StatusOK
	if _, err := d.TrackByID(t.Context(), "1"); err != nil {
		t.Fatal(err)
	}
	status = http.StatusInternalServerError
	for range 4 {
		_, _ = d.TrackByID(t.Context(), "1")
	}
	if _, err := d.TrackByID(t.Context(), "1"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeezer_AQuoteInAnArtistOrTitleDoesNotBreakTheAlbumQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != `artist:"Guns N  Roses" album:"Appetite  For  Destruction"` {
			t.Errorf("query %q", r.URL.Query().Get("q"))
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	if _, err := fastDeezer(srv).SearchAlbums(t.Context(), `Guns N" Roses`, `Appetite "For" Destruction`); err != nil {
		t.Fatal(err)
	}
}

func TestDeezer_AThrottleAnswerResetsTheServerErrorCount(t *testing.T) {
	status, throttledOnce := http.StatusInternalServerError, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status == http.StatusTooManyRequests && !throttledOnce {
			throttledOnce = true
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if status == http.StatusTooManyRequests {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(deezerTrackJSON))
		}
	}))
	defer srv.Close()
	d := fastDeezer(srv)
	for range 4 {
		_, _ = d.TrackByID(t.Context(), "1")
	}
	status = http.StatusTooManyRequests
	if _, err := d.TrackByID(t.Context(), "1"); err != nil {
		t.Fatal(err)
	}
	status = http.StatusInternalServerError
	for range 4 {
		if _, err := d.TrackByID(t.Context(), "1"); errors.Is(err, errCatalogBlocked) {
			t.Fatalf("err = %v", err)
		}
	}
}
