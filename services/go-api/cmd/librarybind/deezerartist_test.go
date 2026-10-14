package main

import (
	"altune/go-api/internal/catalog/domain"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fastDeezerArtists(srv *httptest.Server) deezerArtistSource {
	client := newCatalogClient(srv.Client(), srv.URL, deezerUserAgent, 1000, isDeezerThrottle)
	client.backoff = time.Millisecond
	return deezerArtistSource{client: client}
}

func deezerArtistServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2.0/search/artist":
			_, _ = w.Write([]byte(`{"data":[{"id":6,"name":"Rush"},{"id":9,"name":"Rush Hour"}]}`))
		case "/2.0/artist/6/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"Moving Pictures"},{"title":"Permanent Waves"}]}`))
		case "/2.0/artist/6/top":
			_, _ = w.Write([]byte(`{"data":[{"title":"Limelight"}]}`))
		case "/2.0/search/track":
			if !strings.Contains(r.URL.Query().Get("q"), `artist:"Rush" track:"Tom Sawyer"`) {
				t.Errorf("q = %q", r.URL.Query().Get("q"))
			}
			_, _ = w.Write([]byte(`{"data":[
				{"id":11,"title":"Tom Sawyer","duration":274,"artist":{"id":6,"name":"Rush"},"album":{"title":"Moving Pictures"}},
				{"id":12,"title":"Tom Sawyer","duration":274,"artist":{"id":77,"name":"Rush"},"album":{"title":"Cover Night"}},
				{"id":13,"title":"Tom Sawyer (Live)","duration":301,"artist":{"id":6,"name":"Rush"},"album":{"title":"R40 Live"}}]}`))
		case "/2.0/track/11":
			_, _ = w.Write([]byte(`{"id":11,"title":"Tom Sawyer","isrc":"CAR198100001","duration":274,"artist":{"name":"Rush"},"album":{"title":"Moving Pictures"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestDeezerArtistSource_FindsTheArtistThenTheSongInsideTheirDiscography(t *testing.T) {
	srv := deezerArtistServer(t)
	defer srv.Close()
	source := fastDeezerArtists(srv)

	artists, err := source.FindArtists(t.Context(), "Rush")
	if err != nil || len(artists) != 2 || artists[0] != (providerArtist{ID: "6", Name: "Rush"}) {
		t.Fatalf("artists = %+v, %v", artists, err)
	}
	view, err := source.Catalog(t.Context(), artists[0])
	if err != nil || len(view.Albums) != 2 || view.Titles[0] != "Limelight" {
		t.Fatalf("view = %+v, %v", view, err)
	}
	songs, err := source.Songs(t.Context(), artists[0], view, rushTomSawyer(), domain.CleanSearchKeys("Tom Sawyer", "Rush", ""))
	if err != nil || len(songs) != 1 || songs[0].ID != "11" || songs[0].ISRC != "CAR198100001" {
		t.Fatalf("songs = %+v, %v (only the artist's own fitting recording is expanded)", songs, err)
	}
}

func TestDeezerArtistSource_SixPlausibleRecordingsAreTooManyToTellApart(t *testing.T) {
	var hits []string
	for i := 0; i < 6; i++ {
		hits = append(hits, `{"id":`+strconv.Itoa(20+i)+`,"title":"Tom Sawyer","duration":274,"artist":{"id":6,"name":"Rush"},"album":{"title":"A"}}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/search/track" {
			t.Errorf("expanded %q before refusing", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[` + strings.Join(hits, ",") + `]}`))
	}))
	defer srv.Close()
	_, err := fastDeezerArtists(srv).Songs(t.Context(), providerArtist{ID: "6", Name: "Rush"}, catalogView{}, rushTomSawyer(), domain.CleanSearchKeys("Tom Sawyer", "Rush", ""))
	if !errors.Is(err, errTooManyMatches) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeezerArtistSource_AbortsOnForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	if _, err := fastDeezerArtists(srv).FindArtists(t.Context(), "Rush"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v, want blocked", err)
	}
}
