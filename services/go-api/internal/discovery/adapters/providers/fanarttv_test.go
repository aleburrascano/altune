package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestFanartTvArtworkResolver_Resolve_ArtistThumb(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"artistthumb": [
				{"url": "https://assets.fanart.tv/fanart/music/artist-thumb.jpg"}
			],
			"artistbackground": [
				{"url": "https://assets.fanart.tv/fanart/music/artist-bg.jpg"}
			]
		}`))
	}))
	defer server.Close()

	resolver := NewFanartTvArtworkResolver(newTestClient(server.URL), "test-api-key")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "a74b1b7f-mbid")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	// artistthumb takes priority over artistbackground for artist kind
	if url != "https://assets.fanart.tv/fanart/music/artist-thumb.jpg" {
		t.Errorf("resolve URL: got %q, want artistthumb URL", url)
	}
}

func TestFanartTvArtworkResolver_Resolve_ArtistBackground_Fallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No artistthumb, only artistbackground
		w.Write([]byte(`{
			"artistbackground": [
				{"url": "https://assets.fanart.tv/fanart/music/artist-bg.jpg"}
			]
		}`))
	}))
	defer server.Close()

	resolver := NewFanartTvArtworkResolver(newTestClient(server.URL), "test-api-key")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "some-mbid")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://assets.fanart.tv/fanart/music/artist-bg.jpg" {
		t.Errorf("resolve URL: got %q, want artistbackground fallback", url)
	}
}

func TestFanartTvArtworkResolver_Resolve_AlbumCover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"albumcover": [
				{"url": "https://assets.fanart.tv/fanart/music/album-cover.jpg"}
			]
		}`))
	}))
	defer server.Close()

	resolver := NewFanartTvArtworkResolver(newTestClient(server.URL), "test-api-key")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindAlbum, "OK Computer", "Radiohead", "rg-mbid")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://assets.fanart.tv/fanart/music/album-cover.jpg" {
		t.Errorf("resolve URL: got %q, want albumcover URL", url)
	}
}

func TestFanartTvArtworkResolver_Resolve_EmptyMbid(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	}))
	defer server.Close()

	resolver := NewFanartTvArtworkResolver(newTestClient(server.URL), "test-api-key")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when mbid is empty, got %q", url)
	}
	if callCount != 0 {
		t.Errorf("expected no HTTP calls when mbid is empty, got %d", callCount)
	}
}

func TestFanartTvArtworkResolver_Resolve_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	resolver := NewFanartTvArtworkResolver(newTestClient(server.URL), "test-api-key")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Unknown", "", "unknown-mbid")
	if err != nil {
		t.Fatalf("expected nil error on HTTP 404, got: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on HTTP 404, got %q", url)
	}
}
