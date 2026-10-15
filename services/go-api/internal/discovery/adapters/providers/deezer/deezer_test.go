package deezer

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDeezerAdapter_Search_Tracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/track") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 123456,
				"title": "Bohemian Rhapsody",
				"link": "https://www.deezer.com/track/123456",
				"duration": 355,
				"isrc": "GBAYE7500101",
				"rank": 150000,
				"artist": {"id": 1, "name": "Queen"},
				"album": {"id": 10, "title": "A Night at the Opera", "cover_big": "https://cdn.deezer.com/cover.jpg"}
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "bohemian rhapsody", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Bohemian Rhapsody" {
		t.Errorf("title: got %q, want %q", r.Title, "Bohemian Rhapsody")
	}
	if r.Subtitle != "Queen" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Queen")
	}
	if r.ImageURL != "https://cdn.deezer.com/cover.jpg" {
		t.Errorf("imageURL: got %q, want album cover_big", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderDeezer {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderDeezer)
	}
	if r.Sources[0].ExternalID != "123456" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "123456")
	}
	if r.ISRC != "GBAYE7500101" {
		t.Errorf("ISRC: got %q, want %q", r.ISRC, "GBAYE7500101")
	}
	if r.Extras["album"] != "A Night at the Opera" {
		t.Errorf("extras.album: got %v, want %q", r.Extras["album"], "A Night at the Opera")
	}
	if r.Duration != 355 {
		t.Errorf("Duration: got %d, want 355", r.Duration)
	}
	if r.ProviderRank != 150000 {
		t.Errorf("ProviderRank: got %d, want 150000", r.ProviderRank)
	}
}

func TestDeezerAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/artist") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 42,
				"name": "Radiohead",
				"link": "https://www.deezer.com/artist/42",
				"picture_big": "https://cdn.deezer.com/artist.jpg",
				"nb_fan": 5000000
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "radiohead", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "Radiohead" {
		t.Errorf("title: got %q, want %q", r.Title, "Radiohead")
	}
	if r.ImageURL != "https://cdn.deezer.com/artist.jpg" {
		t.Errorf("imageURL: got %q, want picture_big", r.ImageURL)
	}
	if r.FanCount != 5000000 {
		t.Errorf("FanCount: got %d, want 5000000", r.FanCount)
	}
}

func TestDeezerAdapter_Search_Albums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/album") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 99,
				"title": "OK Computer",
				"link": "https://www.deezer.com/album/99",
				"cover_big": "https://cdn.deezer.com/album.jpg",
				"artist": {"id": 42, "name": "Radiohead"},
				"record_type": "album",
				"release_date": "1997-05-21",
				"nb_tracks": 12,
				"nb_fan": 50000,
				"genre_id": 152
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "ok computer", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindAlbum {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindAlbum)
	}
	if r.Title != "OK Computer" {
		t.Errorf("title: got %q, want %q", r.Title, "OK Computer")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.ImageURL != "https://cdn.deezer.com/album.jpg" {
		t.Errorf("imageURL: got %q, want cover_big", r.ImageURL)
	}
	if r.RecordType != "album" {
		t.Errorf("RecordType: got %v, want %q", r.RecordType, "album")
	}
	if r.ReleaseDate != "1997-05-21" {
		t.Errorf("ReleaseDate: got %q, want %q", r.ReleaseDate, "1997-05-21")
	}
	if r.TrackCount != 12 {
		t.Errorf("TrackCount: got %d, want 12", r.TrackCount)
	}
	if r.FanCount != 50000 {
		t.Errorf("FanCount: got %d, want 50000", r.FanCount)
	}
	if gid, ok := r.Extras["genre_id"].(int); !ok || gid != 152 {
		t.Errorf("extras.genre_id: got %v (%T), want 152", r.Extras["genre_id"], r.Extras["genre_id"])
	}
}

func TestDeezerAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected an error when all attempted kinds fail on HTTP 500, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

func TestDeezerAdapter_Search_Track_MissingPopularity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 999,
				"title": "Unknown Track",
				"link": "https://www.deezer.com/track/999",
				"duration": 180,
				"artist": {"id": 1, "name": "Artist"}
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "unknown", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.ProviderRank != 0 {
		t.Errorf("extras should not contain 'rank' when API returns 0")
	}
	if r.FanCount != 0 {
		t.Errorf("extras should not contain 'nb_fan' when API returns 0")
	}
}

func TestDeezerAdapter_GetAlbumTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/album/99/tracks") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [
				{
					"id": 201,
					"title": "Airbag",
					"link": "https://www.deezer.com/track/201",
					"duration": 284,
					"artist": {"id": 42, "name": "Radiohead"}
				},
				{
					"id": 202,
					"title": "Paranoid Android",
					"link": "https://www.deezer.com/track/202",
					"duration": 383,
					"artist": {"id": 42, "name": "Radiohead"}
				}
			]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetAlbumTracks(context.Background(), domain.ProviderDeezer, "99")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(results))
	}
	if results[0].Title != "Airbag" {
		t.Errorf("first track title: got %q, want %q", results[0].Title, "Airbag")
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Errorf("first track kind: got %v, want %v", results[0].Kind, domain.ResultKindTrack)
	}
	if results[1].Title != "Paranoid Android" {
		t.Errorf("second track title: got %q, want %q", results[1].Title, "Paranoid Android")
	}
}

func TestDeezerAdapter_GetArtistTopTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/artist/42/top") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 301,
				"title": "Creep",
				"link": "https://www.deezer.com/track/301",
				"duration": 236,
				"artist": {"id": 42, "name": "Radiohead"}
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetArtistTopTracks(context.Background(), domain.ProviderDeezer, "42")
	if err != nil {
		t.Fatalf("GetArtistTopTracks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 track, got %d", len(results))
	}
	if results[0].Title != "Creep" {
		t.Errorf("track title: got %q, want %q", results[0].Title, "Creep")
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Errorf("track kind: got %v, want %v", results[0].Kind, domain.ResultKindTrack)
	}
	if results[0].Sources[0].ExternalID != "301" {
		t.Errorf("source externalID: got %q, want %q", results[0].Sources[0].ExternalID, "301")
	}
}

func TestDeezerAdapter_GetArtistAlbums_paginates(t *testing.T) {
	var indexes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/artist/42/albums") {
			http.NotFound(w, r)
			return
		}
		indexes = append(indexes, r.URL.Query().Get("index"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("index") == "0" {
			w.Write([]byte(`{
				"data": [
					{"id": 1, "title": "First", "artist": {"id": 42, "name": "Radiohead"}},
					{"id": 2, "title": "Second", "artist": {"id": 42, "name": "Radiohead"}}
				],
				"total": 3,
				"next": "https://api.deezer.com/artist/42/albums?limit=100&index=100"
			}`))
			return
		}
		w.Write([]byte(`{
			"data": [{"id": 3, "title": "Third", "artist": {"id": 42, "name": "Radiohead"}}],
			"total": 3
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderDeezer, "42")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(indexes) != 2 || indexes[0] != "0" || indexes[1] != "100" {
		t.Errorf("index params: got %v, want [0 100]", indexes)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 albums across pages, got %d", len(results))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if results[i].Title != want {
			t.Errorf("album[%d]: got %q, want %q (pages appended in request order)", i, results[i].Title, want)
		}
	}
}

const deezerQuotaErrorJSON = `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`

func TestDeezerAdapter_Search_QuotaErrorBodySurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deezerQuotaErrorJSON))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected an error on a 200 quota-error body, got nil (silent empty success)")
	}
	if !strings.Contains(err.Error(), "Quota limit exceeded") {
		t.Errorf("err = %v, want the Deezer error message surfaced", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestDeezerAdapter_GetArtistAlbums_QuotaErrorBodySurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deezerQuotaErrorJSON))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	albums, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderDeezer, "42")
	if err == nil {
		t.Fatal("expected an error on a 200 quota-error body, got nil (empty discography as truth)")
	}
	if len(albums) != 0 {
		t.Errorf("expected 0 albums, got %d", len(albums))
	}
}

func TestDeezerAdapter_GetArtistAlbums_laterPageErrorKeepsEarlierPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/artist/42/albums") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("index") != "0" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": 1, "title": "First", "artist": {"id": 42, "name": "Radiohead"}},
				{"id": 2, "title": "Second", "artist": {"id": 42, "name": "Radiohead"}}
			],
			"next": "https://api.deezer.com/artist/42/albums?limit=100&index=100"
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderDeezer, "42")
	var partial *domain.PartialResultError
	if !errors.As(err, &partial) || partial.Page != 1 {
		t.Fatalf("expected a PartialResultError for page 1 alongside the earlier pages, got: %v", err)
	}
	if len(results) != 2 || results[0].Title != "First" || results[1].Title != "Second" {
		t.Fatalf("results = %+v, want the 2 page-1 albums kept", results)
	}
}

func TestDeezerStructuredQuery_stripsEmbeddedQuotes(t *testing.T) {
	got := deezerStructuredQuery(`The "Best" Band`, `Hello`, domain.ResultKindTrack)
	want := `artist:"The Best Band" track:"Hello"`
	if got != want {
		t.Errorf("query = %q, want %q (embedded quotes stripped)", got, want)
	}
}

func TestDeezerAdapter_FetchCharts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/chart/0/tracks"):
			_, _ = w.Write([]byte(`{"data": [
				{"id": 1, "title": "Top Track", "rank": 900000},
				{"id": 2, "title": "Metricless Track"}
			]}`))
		case strings.HasPrefix(r.URL.Path, "/chart/0/artists"):
			_, _ = w.Write([]byte(`{"data": [{"id": 3, "name": "Top Artist", "nb_fan": 5000}]}`))
		case strings.HasPrefix(r.URL.Path, "/chart/0/albums"):
			_, _ = w.Write([]byte(`{"data": [{"id": 4, "title": "Top Album"}, {"id": 5, "title": ""}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	entries, err := adapter.FetchCharts(context.Background(), 10)
	if err != nil {
		t.Fatalf("FetchCharts: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4 (blank terms dropped)", len(entries))
	}
	byTerm := map[string]domain.VocabularyEntry{}
	for _, e := range entries {
		byTerm[e.Term] = e
	}
	if e := byTerm["Top Track"]; e.Kind != "track" || e.Popularity != 900000 {
		t.Errorf("Top Track = %+v, want kind=track popularity=rank", e)
	}
	if e := byTerm["Metricless Track"]; e.Popularity != 999 {
		t.Errorf("Metricless Track popularity = %d, want 999 (1000 - position fallback)", e.Popularity)
	}
	if e := byTerm["Top Artist"]; e.Kind != "artist" || e.Popularity != 5000 {
		t.Errorf("Top Artist = %+v, want kind=artist popularity=nb_fan", e)
	}
	if e := byTerm["Top Album"]; e.Kind != "album" || e.Popularity != 1000 {
		t.Errorf("Top Album = %+v, want kind=album popularity=1000 (position fallback)", e)
	}
}

func TestDeezerAdapter_FetchCharts_failedKindSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/chart/0/tracks") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": 3, "name": "Artist", "nb_fan": 1}, {"id": 4, "title": "Album"}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	entries, err := adapter.FetchCharts(context.Background(), 10)
	if err != nil {
		t.Fatalf("FetchCharts must not fail when one chart kind fails: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected the surviving chart kinds' entries")
	}
	for _, e := range entries {
		if e.Kind == "track" {
			t.Errorf("unexpected track entry %+v from the failed kind", e)
		}
	}
}

func TestDeezerAdapter_SearchStructured_sendsAdvancedQuery(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": 1, "title": "Hello", "artist": {"id": 2, "name": "Adele"}}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.SearchStructured(context.Background(), "Adele", "Hello", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("SearchStructured: %v", err)
	}
	if gotQuery != `artist:"Adele" track:"Hello"` {
		t.Errorf("q = %q, want the advanced artist/track query", gotQuery)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}

func TestDeezerAdapter_SearchStructured_failedKindSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/album") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": 1, "title": "Hello", "artist": {"id": 2, "name": "Adele"}}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.SearchStructured(context.Background(), "Adele", "Hello", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("SearchStructured must skip a failed kind, got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want the surviving kind's 1 result", len(results))
	}
}

func TestDeezerAdapter_LookupTrackFeatured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"contributors": [
			{"id": 1, "name": "Main Artist", "role": "Main"},
			{"id": 2, "name": "Guest One", "role": "Featured"}
		]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	feats, err := adapter.LookupTrackFeatured(context.Background(), "123")
	if err != nil {
		t.Fatalf("LookupTrackFeatured: %v", err)
	}
	if len(feats) != 1 || feats[0].Name != "Guest One" || feats[0].DeezerID != 2 {
		t.Errorf("feats = %+v, want the single Featured contributor", feats)
	}
}

func TestDeezerAdapter_LookupTrackFeatured_quotaErrorSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deezerQuotaErrorJSON))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	if _, err := adapter.LookupTrackFeatured(context.Background(), "123"); err == nil {
		t.Fatal("expected the in-band quota error to surface (200 envelope must not decode as empty success)")
	}
}

func TestDeezerAdapter_GetArtistAlbums_nextDrivenPagination(t *testing.T) {
	var indexes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexes = append(indexes, r.URL.Query().Get("index"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("index") == "0" {
			_, _ = w.Write([]byte(`{
				"data": [{"id": 1, "title": "First", "artist": {"id": 42, "name": "A"}}],
				"next": "https://api.deezer.com/artist/42/albums?limit=100&index=100"
			}`))
			return
		}
		_, _ = w.Write([]byte(`{"data": [{"id": 2, "title": "Second", "artist": {"id": 42, "name": "A"}}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderDeezer, "42")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(indexes) != 2 || indexes[0] != "0" || indexes[1] != "100" {
		t.Errorf("indexes = %v, want [0 100] (next-driven walk stops when next is empty)", indexes)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 across pages", len(results))
	}
}

func TestDeezerAdapter_GetArtistAlbums_capsAtMaxPages(t *testing.T) {
	var pages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{"id": 1, "title": "Loop", "artist": {"id": 42, "name": "A"}}],
			"next": "https://api.deezer.com/artist/42/albums?limit=100&index=100"
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderDeezer, "42")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if pages != deezerMaxDiscographyPages {
		t.Errorf("pages fetched = %d, want the %d-page cap", pages, deezerMaxDiscographyPages)
	}
	if len(results) != deezerMaxDiscographyPages {
		t.Errorf("results = %d, want one per capped page", len(results))
	}
}

func TestMapDeezerResult_unicodeSurvives(t *testing.T) {
	item := deezerItem{
		ID:    1,
		Title: "İstanbul 東京 🎵 «quotes»",
		Artist: &deezerRef{
			ID: 2, Name: `The "Best" Band`,
		},
	}
	r := mapDeezerResult(item, domain.ResultKindTrack)
	if r.Title != "İstanbul 東京 🎵 «quotes»" {
		t.Errorf("title = %q, want unicode preserved verbatim", r.Title)
	}
	if r.Subtitle != `The "Best" Band` {
		t.Errorf("subtitle = %q, want embedded quotes preserved in mapping (stripping is query-side only)", r.Subtitle)
	}
}

func TestMapDeezerResult_missingOptionalFields(t *testing.T) {
	r := mapDeezerResult(deezerItem{ID: 9, Title: "Orphan"}, domain.ResultKindTrack)
	if r.Subtitle != "" || r.Album != "" || r.DeezerAlbumID != "" || r.ImageURL != "" {
		t.Errorf("result = %+v, want zero optional fields for a bare item", r)
	}
	if r.Sources[0].ExternalID != "9" {
		t.Errorf("ExternalID = %q, want the numeric id stringified", r.Sources[0].ExternalID)
	}
}

func TestDeezerAdapter_GetAlbumTracks_readsEveryPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		index, _ := strconv.Atoi(r.URL.Query().Get("index"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		const total = 120
		var items []string
		for i := index; i < min(index+limit, total); i++ {
			items = append(items, fmt.Sprintf(`{"id": %d, "title": "Track %d", "artist": {"id": 1, "name": "Nirvana"}}`, 1000+i, i))
		}
		next := ""
		if index+limit < total {
			next = fmt.Sprintf(`, "next": "%s/album/99/tracks?index=%d"`, "http://deezer.test", index+limit)
		}
		fmt.Fprintf(w, `{"data": [%s], "total": %d%s}`, strings.Join(items, ","), total, next)
	}))
	defer server.Close()

	results, err := NewDeezerAdapter(providertest.NewClient(server.URL)).GetAlbumTracks(t.Context(), domain.ProviderDeezer, "99")
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 120 || results[119].Title != "Track 119" {
		t.Fatalf("got %d tracks, want all 120 in order", len(results))
	}
}

func TestMapDeezerResult_CarriesTheProvidersTrackPositionAndDiskNumber(t *testing.T) {
	r := mapDeezerResult(deezerItem{ID: 7, Title: "Bonus One", TrackPosition: 1, DiskNumber: 2}, domain.ResultKindTrack)
	if r.TrackPosition != (domain.TrackPosition{Disc: 2, Track: 1}) {
		t.Errorf("track position = %+v, want track 1 disc 2", r.TrackPosition)
	}
}

func searchRecordedPayload(t *testing.T, kind domain.ResultKind, fixture string) domain.SearchResult {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data": [%s], "total": 1}`, raw)
	}))
	defer server.Close()
	results, err := NewDeezerAdapter(providertest.NewClient(server.URL)).Search(t.Context(), "recorded", map[domain.ResultKind]bool{kind: true})
	if err != nil || len(results) != 1 {
		t.Fatalf("Search = %d results, err %v, want exactly the recorded one", len(results), err)
	}
	return results[0]
}

func TestSearch_RecordedTrackPayloadFillsTheTypedContract(t *testing.T) {
	r := searchRecordedPayload(t, domain.ResultKindTrack, "recorded_track_with_featured_contributor.json")

	wantContributors := []domain.Contributor{
		{ProviderID: "12178", Name: "Calvin Harris", Role: domain.ContributorMain},
		{ProviderID: "564", Name: "Rihanna", Role: domain.ContributorFeatured},
	}
	checks := []struct {
		field     string
		got, want any
	}{
		{"ISRC", r.ISRC, "GBARL1600460"},
		{"TrackPosition", r.TrackPosition, domain.TrackPosition{Disc: 1, Track: 1}},
		{"Duration", r.Duration, 222},
		{"Contributors", r.Contributors, wantContributors},
		{"Explicit", r.Explicit != nil && !*r.Explicit, true},
		{"Album", r.Album, "This Is What You Came For"},
		{"Extras track_number", r.Extras["track_number"], nil},
		{"Extras duration", r.Extras["duration"], nil},
		{"Extras explicit", r.Extras["explicit"], nil},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.field, c.got, c.want)
		}
	}
}

func TestSearch_RecordedTrackWithSeveralMainContributorsKeepsEachOnce(t *testing.T) {
	r := searchRecordedPayload(t, domain.ResultKindTrack, "recorded_track_with_several_main_contributors.json")

	want := []domain.Contributor{
		{ProviderID: "27", Name: "Daft Punk", Role: domain.ContributorMain},
		{ProviderID: "103", Name: "Pharrell Williams", Role: domain.ContributorMain},
		{ProviderID: "7207", Name: "Nile Rodgers", Role: domain.ContributorMain},
	}
	if !reflect.DeepEqual(r.Contributors, want) {
		t.Errorf("Contributors = %#v, want %#v", r.Contributors, want)
	}
	if r.TrackPosition != (domain.TrackPosition{Disc: 1, Track: 8}) {
		t.Errorf("TrackPosition = %+v, want disc 1 track 8", r.TrackPosition)
	}
}

func TestSearch_RecordedAlbumPayloadFillsTheTypedContract(t *testing.T) {
	r := searchRecordedPayload(t, domain.ResultKindAlbum, "recorded_album_detail.json")

	checks := []struct {
		field     string
		got, want any
	}{
		{"UPC", r.UPC, "724384960650"},
		{"RecordType", r.RecordType, domain.RecordTypeAlbum},
		{"TrackCount", r.TrackCount, 14},
		{"Contributors", r.Contributors, []domain.Contributor{{ProviderID: "27", Name: "Daft Punk", Role: domain.ContributorMain}}},
		{"Explicit", r.Explicit != nil && !*r.Explicit, true},
		{"ISRC", r.ISRC, ""},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.field, c.got, c.want)
		}
	}
}

func TestMapDeezerResult_RecordTypeIsParsedCaseInsensitivelyAgainstDeezersVocabulary(t *testing.T) {
	tests := []struct {
		raw  string
		want domain.RecordType
	}{
		{"album", domain.RecordTypeAlbum},
		{"EP", domain.RecordTypeEP},
		{" Single ", domain.RecordTypeSingle},
		{"compilation", domain.RecordTypeCompilation},
		{"mixtape", domain.RecordTypeUnknown},
	}
	for _, tt := range tests {
		if got := mapDeezerResult(deezerItem{ID: 1, Title: "A", RecordType: tt.raw}, domain.ResultKindAlbum).RecordType; got != tt.want {
			t.Errorf("record_type %q = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestMapDeezerResult_ISRCIsCanonicalOrEmpty(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"gb-arl-16-00460", "GBARL1600460"},
		{"not-an-isrc", ""},
	}
	for _, tt := range tests {
		if got := mapDeezerResult(deezerItem{ID: 1, Title: "T", ISRC: tt.raw}, domain.ResultKindTrack).ISRC; got != tt.want {
			t.Errorf("isrc %q = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
