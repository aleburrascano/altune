package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

const recordingJSON = `{"id":"rec-1","title":"Careless Whisper",
	"artist-credit":[{"name":"Wham!","joinphrase":" feat. "},{"name":"George Michael","joinphrase":""}],
	"isrcs":["GBAYE8400001"]}`

func releaseJSON(groupID, title, primary, date string) string {
	return `{"release-group":{"id":"` + groupID + `","title":"` + title + `","primary-type":"` + primary +
		`","secondary-types":[],"first-release-date":"` + date + `"},"artist-credit":[{"name":"Wham!","joinphrase":""}]}`
}

const releaseGroupJSON = `{"release-count":1,"releases":[{"release-group":{"id":"rg-a","title":"Make It Big","primary-type":"Album",
	"secondary-types":["Compilation"],"first-release-date":"1984-11-01"},"artist-credit":[{"name":"Wham!","joinphrase":""}]}]}`

func fastMusicBrainz(srv *httptest.Server) musicBrainz {
	m := newMusicBrainz(srv.Client(), srv.URL, 1000)
	m.client.backoff = time.Millisecond
	return m
}

func TestMusicBrainz_ReadsARecordingWithItsCreditsAndIsrcs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/2/recording/rec-1" || r.URL.Query().Get("inc") != "isrcs artist-credits" {
			t.Errorf("request %s", r.URL)
		}
		_, _ = w.Write([]byte(recordingJSON))
	}))
	defer srv.Close()
	got, err := fastMusicBrainz(srv).Recording(t.Context(), "rec-1")
	if err != nil || got.Title != "Careless Whisper" || got.Artist != "Wham! feat. George Michael" || got.Primary != "Wham!" || !got.hasISRC("gb-aye-8400001") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMusicBrainz_ReadsReleaseGroupsWithTheirTypesAndFirstReleaseDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/ws/2/release" || query.Get("recording") != "rec-1" || query.Get("status") != "official" || query.Get("inc") != "release-groups artist-credits" {
			t.Errorf("request %s", r.URL)
		}
		_, _ = w.Write([]byte(releaseGroupJSON))
	}))
	defer srv.Close()
	got, err := fastMusicBrainz(srv).ReleaseGroups(t.Context(), "rec-1")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if g := got[0]; g.ID != "rg-a" || g.Title != "Make It Big" || g.Artist != "Wham!" || g.PrimaryType != "Album" ||
		!slices.Equal(g.SecondaryTypes, []string{"Compilation"}) || g.FirstReleaseDate != "1984-11-01" {
		t.Fatalf("got %+v", g)
	}
}

func TestMusicBrainz_PagesThroughEveryReleaseSoAnAlbumOnALaterPageIsFound(t *testing.T) {
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		page1 := releaseJSON("rg-comp", "Hits", "Album", "1990") + "," + releaseJSON("rg-comp", "Hits", "Album", "1990")
		page2 := releaseJSON("rg-lp", "Studio", "Album", "1975")
		if offset == "0" {
			_, _ = w.Write([]byte(`{"release-count":3,"releases":[` + page1 + `]}`))
			return
		}
		_, _ = w.Write([]byte(`{"release-count":3,"releases":[` + page2 + `]}`))
	}))
	defer srv.Close()
	got, err := fastMusicBrainz(srv).ReleaseGroups(t.Context(), "rec-1")
	if err != nil || len(got) != 2 || got[1].ID != "rg-lp" || !slices.Equal(offsets, []string{"0", "2"}) {
		t.Fatalf("got %+v offsets %v, %v", got, offsets, err)
	}
	if chosen, ok := chooseRelease(got); !ok || chosen.Group.ID != "rg-lp" {
		t.Fatalf("chosen %+v", chosen)
	}
}

func TestMusicBrainz_SearchesRecordingsByIsrc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != `isrc:"GBAYE8400001"` {
			t.Errorf("query %q", r.URL.Query().Get("query"))
		}
		_, _ = w.Write([]byte(`{"recordings":[` + recordingJSON + `]}`))
	}))
	defer srv.Close()
	got, err := fastMusicBrainz(srv).RecordingsByISRC(t.Context(), "gb-aye-8400001")
	if err != nil || len(got) != 1 || got[0].ID != "rec-1" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMusicBrainz_UnknownIdIsUnknownAndForbiddenIsBlocked(t *testing.T) {
	status := http.StatusNotFound
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	m := fastMusicBrainz(srv)
	if _, err := m.Recording(t.Context(), "x"); !errors.Is(err, errCatalogUnknown) {
		t.Fatalf("err = %v", err)
	}
	status = http.StatusForbidden
	if _, err := m.ReleaseGroups(t.Context(), "x"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestMusicBrainz_BacksOffOnA503ThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(releaseGroupJSON))
	}))
	defer srv.Close()
	if _, err := fastMusicBrainz(srv).ReleaseGroups(t.Context(), "rec-1"); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
