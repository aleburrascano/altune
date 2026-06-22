package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// deezerEnrichmentServer routes the resolve search + the /track|/album detail
// fetches to canned bodies captured from the live probe (docs/providers/deezer.md
// §4, 2026-06-22).
func deezerEnrichmentServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/track"):
			w.Write([]byte(`{"data":[{"id":1109731,"title":"Lose Yourself","link":"https://www.deezer.com/track/1109731","artist":{"id":13,"name":"Eminem"}}]}`))
		case strings.HasPrefix(r.URL.Path, "/search/album"):
			w.Write([]byte(`{"data":[{"id":302127,"title":"Discovery","link":"https://www.deezer.com/album/302127","artist":{"id":27,"name":"Daft Punk"}}]}`))
		case r.URL.Path == "/track/1109731":
			w.Write([]byte(`{"id":1109731,"title":"Lose Yourself","bpm":171.6,"gain":-8.3,"explicit_lyrics":true,"isrc":"USIR10211570"}`))
		case r.URL.Path == "/album/302127":
			w.Write([]byte(`{"id":302127,"title":"Discovery","upc":"724384960650","label":"Daft Life Ltd./ADA France","record_type":"album","genres":{"data":[{"id":106,"name":"Electro"},{"id":106,"name":"Electro"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestDeezerAdapter_ResolveAndLookupTrack(t *testing.T) {
	server := deezerEnrichmentServer()
	defer server.Close()
	adapter := NewDeezerAdapter(newTestClient(server.URL))

	id, err := adapter.ResolveID(context.Background(), domain.ResultKindTrack, "Eminem", "Lose Yourself")
	if err != nil {
		t.Fatalf("ResolveID: %v", err)
	}
	if id != "1109731" {
		t.Fatalf("ResolveID: got %q, want 1109731", id)
	}

	e, err := adapter.Lookup(context.Background(), domain.ResultKindTrack, id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.BPM != 172 { // 171.6 rounds to 172
		t.Errorf("BPM: got %d, want 172", e.BPM)
	}
	if e.Gain != -8.3 {
		t.Errorf("Gain: got %v, want -8.3", e.Gain)
	}
	if !e.Explicit {
		t.Errorf("Explicit: got false, want true")
	}
}

func TestDeezerAdapter_ResolveAndLookupAlbum(t *testing.T) {
	server := deezerEnrichmentServer()
	defer server.Close()
	adapter := NewDeezerAdapter(newTestClient(server.URL))

	id, err := adapter.ResolveID(context.Background(), domain.ResultKindAlbum, "Daft Punk", "Discovery")
	if err != nil {
		t.Fatalf("ResolveID: %v", err)
	}
	if id != "302127" {
		t.Fatalf("ResolveID: got %q, want 302127", id)
	}

	e, err := adapter.Lookup(context.Background(), domain.ResultKindAlbum, id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.Label != "Daft Life Ltd./ADA France" {
		t.Errorf("Label: got %q", e.Label)
	}
	if e.UPC != "724384960650" {
		t.Errorf("UPC: got %q", e.UPC)
	}
	if e.RecordType != "album" {
		t.Errorf("RecordType: got %q", e.RecordType)
	}
	// Duplicate genre in the fixture must be deduped to one.
	if len(e.Genres) != 1 || e.Genres[0] != "Electro" {
		t.Errorf("Genres: got %v, want [Electro]", e.Genres)
	}
}

func TestDeezerAdapter_ArtistKindResolvesToEmpty(t *testing.T) {
	server := deezerEnrichmentServer()
	defer server.Close()
	adapter := NewDeezerAdapter(newTestClient(server.URL))

	id, err := adapter.ResolveID(context.Background(), domain.ResultKindArtist, "Daft Punk", "Daft Punk")
	if err != nil {
		t.Fatalf("ResolveID: %v", err)
	}
	if id != "" {
		t.Errorf("artist kind should not resolve, got %q", id)
	}
}

// TestDeezerAdapter_Resolve_PrefersXLArtwork verifies the cap-2 artwork bump:
// the ArtworkResolver returns the 1000px _xl cover when present.
func TestDeezerAdapter_Resolve_PrefersXLArtwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":302127,"title":"Discovery","cover_big":"https://cdn.deezer.com/500x500.jpg","cover_xl":"https://cdn.deezer.com/1000x1000.jpg","artist":{"id":27,"name":"Daft Punk"}}]}`))
	}))
	defer server.Close()
	adapter := NewDeezerAdapter(newTestClient(server.URL))

	img, err := adapter.Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if img != "https://cdn.deezer.com/1000x1000.jpg" {
		t.Errorf("expected 1000px _xl cover, got %q", img)
	}
}
