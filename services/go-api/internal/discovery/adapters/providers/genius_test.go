package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestGeniusArtworkResolver_Resolve_Song(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header is present
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			t.Errorf("Authorization header: got %q, want %q", auth, "Bearer test-token-123")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"song_art_image_url": "https://images.genius.com/song-art.jpg",
						"header_image_url": "https://images.genius.com/header.jpg",
						"primary_artist": {
							"name": "Radiohead",
							"image_url": "https://images.genius.com/artist.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(newTestClient(server.URL), "test-token-123")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Creep", "Radiohead", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://images.genius.com/song-art.jpg" {
		t.Errorf("resolve URL: got %q, want song_art_image_url", url)
	}
}

func TestGeniusArtworkResolver_Resolve_Song_FallbackToHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"song_art_image_url": "",
						"header_image_url": "https://images.genius.com/header-fallback.jpg",
						"primary_artist": {
							"name": "Radiohead",
							"image_url": "https://images.genius.com/artist.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(newTestClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Creep", "Radiohead", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://images.genius.com/header-fallback.jpg" {
		t.Errorf("resolve URL: got %q, want header_image_url fallback", url)
	}
}

func TestGeniusArtworkResolver_Resolve_Artist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"primary_artist": {
							"name": "Radiohead",
							"image_url": "https://images.genius.com/radiohead-artist.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(newTestClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://images.genius.com/radiohead-artist.jpg" {
		t.Errorf("resolve URL: got %q, want artist image_url", url)
	}
}

func TestGeniusArtworkResolver_Resolve_SkipsDefaultImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"song_art_image_url": "https://images.genius.com/default_cover.png",
						"header_image_url": "https://images.genius.com/no_image_placeholder.jpg",
						"primary_artist": {
							"name": "Test",
							"image_url": "https://images.genius.com/default_avatar.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(newTestClient(server.URL), "token")
	// song_art_image_url contains "default", header contains "no_image" — both should be skipped
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Something", "Test", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when images contain 'default'/'no_image', got %q", url)
	}
}

func TestGeniusArtworkResolver_Resolve_NoSubtitle(t *testing.T) {
	// When kind is not artist and subtitle is empty, Resolve returns empty
	resolver := NewGeniusArtworkResolver(http.DefaultClient, "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Song", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when subtitle is empty for non-artist kind, got %q", url)
	}
}

func TestGeniusArtworkResolver_Resolve_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(newTestClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Song", "Artist", "")
	if err != nil {
		t.Fatalf("expected nil error on HTTP 500, got: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on HTTP 500, got %q", url)
	}
}
