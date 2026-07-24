package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// deezerQuotaErrorJSON is Deezer's in-band error envelope: quota exhaustion
// rides on HTTP 200, so without the envelope check it decodes as an empty
// success and the provider looks healthy while dead.
const deezerQuotaErrorJSON = `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`

func TestDeezerAdapter_Search_QuotaErrorBodySurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deezerQuotaErrorJSON))
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderDeezer, "42")
	if err != nil {
		t.Fatalf("expected the partial set on a later-page failure, got error: %v", err)
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
