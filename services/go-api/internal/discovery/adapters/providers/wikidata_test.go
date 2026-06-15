package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWikidataMbidResolver_Resolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.Write([]byte(`{
			"results": {
				"bindings": [{
					"mbid": {"value": "a74b1b7f-71a5-4011-9441-d0b5e4122711"}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewWikidataMbidResolver(newTestClient(server.URL))
	mbid, err := resolver.Resolve(context.Background(), "https://www.deezer.com/artist/399")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if mbid != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("mbid: got %q, want %q", mbid, "a74b1b7f-71a5-4011-9441-d0b5e4122711")
	}
}

func TestWikidataMbidResolver_Resolve_NonDeezerURL(t *testing.T) {
	// Non-Deezer URLs should return empty without making any HTTP call
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	}))
	defer server.Close()

	resolver := NewWikidataMbidResolver(newTestClient(server.URL))
	mbid, err := resolver.Resolve(context.Background(), "https://www.spotify.com/artist/123")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if mbid != "" {
		t.Errorf("expected empty mbid for non-Deezer URL, got %q", mbid)
	}
	if callCount != 0 {
		t.Errorf("expected no HTTP calls for non-Deezer URL, got %d", callCount)
	}
}

func TestWikidataMbidResolver_Resolve_EmptyBindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.Write([]byte(`{
			"results": {
				"bindings": []
			}
		}`))
	}))
	defer server.Close()

	resolver := NewWikidataMbidResolver(newTestClient(server.URL))
	mbid, err := resolver.Resolve(context.Background(), "https://www.deezer.com/artist/999999")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if mbid != "" {
		t.Errorf("expected empty mbid for empty bindings, got %q", mbid)
	}
}

func TestWikidataMbidResolver_Resolve_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	resolver := NewWikidataMbidResolver(newTestClient(server.URL))
	mbid, err := resolver.Resolve(context.Background(), "https://www.deezer.com/artist/42")
	if err != nil {
		t.Fatalf("expected nil error on HTTP 500, got: %v", err)
	}
	if mbid != "" {
		t.Errorf("expected empty mbid on HTTP 500, got %q", mbid)
	}
}

func TestWikidataMbidResolver_Resolve_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	resolver := NewWikidataMbidResolver(newTestClient(server.URL))
	mbid, err := resolver.Resolve(context.Background(), "https://www.deezer.com/artist/42")
	if err != nil {
		t.Fatalf("expected nil error on HTTP 429, got: %v", err)
	}
	if mbid != "" {
		t.Errorf("expected empty mbid on HTTP 429, got %q", mbid)
	}
}
