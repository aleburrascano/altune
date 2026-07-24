package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// --- charts -----------------------------------------------------------------

func TestDeezerAdapter_FetchCharts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/chart/0/tracks"):
			// rank present → popularity from the metric; rank absent → 1000-position fallback
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	entries, err := adapter.FetchCharts(context.Background(), 10)
	if err != nil {
		t.Fatalf("FetchCharts: %v", err)
	}
	// Blank-term album (id 5) dropped → 4 entries.
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

// --- structured search ------------------------------------------------------

func TestDeezerAdapter_SearchStructured_sendsAdvancedQuery(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": 1, "title": "Hello", "artist": {"id": 2, "name": "Adele"}}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

// --- enrichment lookups -----------------------------------------------------

func TestDeezerAdapter_FetchTrackISRC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/track/123" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 123, "isrc": "GBAYE7500101"}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	isrc, err := adapter.FetchTrackISRC(context.Background(), "123")
	if err != nil {
		t.Fatalf("FetchTrackISRC: %v", err)
	}
	if isrc != "GBAYE7500101" {
		t.Errorf("isrc = %q, want GBAYE7500101", isrc)
	}
}

// FetchTrackISRC's documented policy is empty-on-error (best-effort enrichment,
// the caller degrades) — pin it so a future edit doesn't silently flip it.
func TestDeezerAdapter_FetchTrackISRC_errorIsEmptyNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	isrc, err := adapter.FetchTrackISRC(context.Background(), "123")
	if err != nil || isrc != "" {
		t.Errorf("FetchTrackISRC on 500 = (%q, %v), want (\"\", nil) per the documented degrade policy", isrc, err)
	}
}

func TestDeezerAdapter_FetchFirstTrackID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": 777}, {"id": 888}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	id, err := adapter.FetchFirstTrackID(context.Background(), "10")
	if err != nil {
		t.Fatalf("FetchFirstTrackID: %v", err)
	}
	if id != "777" {
		t.Errorf("id = %q, want the first track id", id)
	}
}

func TestDeezerAdapter_FetchFirstTrackID_emptyAlbum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	id, err := adapter.FetchFirstTrackID(context.Background(), "10")
	if err != nil || id != "" {
		t.Errorf("empty album = (%q, %v), want (\"\", nil)", id, err)
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	if _, err := adapter.LookupTrackFeatured(context.Background(), "123"); err == nil {
		t.Fatal("expected the in-band quota error to surface (200 envelope must not decode as empty success)")
	}
}

// --- pagination edges -------------------------------------------------------

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
		// Last page: data present, next absent → stop.
		_, _ = w.Write([]byte(`{"data": [{"id": 2, "title": "Second", "artist": {"id": 42, "name": "A"}}]}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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
		// next always set — an adversarial/looping server must hit the page cap.
		_, _ = w.Write([]byte(`{
			"data": [{"id": 1, "title": "Loop", "artist": {"id": 42, "name": "A"}}],
			"next": "https://api.deezer.com/artist/42/albums?limit=100&index=100"
		}`))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

// --- parsing edges ----------------------------------------------------------

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
	// Track with no artist, no album, no preview: mapping must not panic and
	// must leave the dependent fields zero.
	r := mapDeezerResult(deezerItem{ID: 9, Title: "Orphan"}, domain.ResultKindTrack)
	if r.Subtitle != "" || r.Album != "" || r.DeezerAlbumID != "" || r.ImageURL != "" {
		t.Errorf("result = %+v, want zero optional fields for a bare item", r)
	}
	if r.Sources[0].ExternalID != "9" {
		t.Errorf("ExternalID = %q, want the numeric id stringified", r.Sources[0].ExternalID)
	}
}
