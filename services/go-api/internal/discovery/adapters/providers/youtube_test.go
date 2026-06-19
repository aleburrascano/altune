package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func newTestYouTubeAdapter(server *httptest.Server) *YouTubeArtworkResolver {
	return &YouTubeArtworkResolver{
		client: &http.Client{
			Transport: &rewriteTransport{base: server.URL},
		},
		apiKey: "test-key",
	}
}

func TestYouTubeArtworkResolver_Resolve_ArtistOnly(t *testing.T) {
	t.Parallel()

	t.Run("returns empty for non-artist kind", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not make HTTP calls for non-artist kinds")
		}))
		defer srv.Close()

		adapter := newTestYouTubeAdapter(srv)
		url, err := adapter.Resolve(context.Background(), domain.ResultKindTrack, "Title", "Artist", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "" {
			t.Errorf("expected empty URL for track kind, got %q", url)
		}
	})

	t.Run("returns high-res thumbnail", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/youtube/v3/search":
				json.NewEncoder(w).Encode(ytSearchResponse{
					Items: []ytSearchItem{{ID: ytSearchID{ChannelID: "UC123"}}},
				})
			case "/youtube/v3/channels":
				json.NewEncoder(w).Encode(ytChannelResponse{
					Items: []ytChannelItem{{
						Snippet: ytChannelSnippet{
							Thumbnails: ytThumbnails{
								Default: ytThumbnail{URL: "https://yt.com/default.jpg"},
								Medium:  ytThumbnail{URL: "https://yt.com/medium.jpg"},
								High:    ytThumbnail{URL: "https://yt.com/high.jpg"},
							},
						},
					}},
				})
			}
		}))
		defer srv.Close()

		adapter := newTestYouTubeAdapter(srv)
		url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "TestArtist", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://yt.com/high.jpg" {
			t.Errorf("expected high-res thumbnail, got %q", url)
		}
	})

	t.Run("falls back to medium when high missing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/youtube/v3/search":
				json.NewEncoder(w).Encode(ytSearchResponse{
					Items: []ytSearchItem{{ID: ytSearchID{ChannelID: "UC456"}}},
				})
			case "/youtube/v3/channels":
				json.NewEncoder(w).Encode(ytChannelResponse{
					Items: []ytChannelItem{{
						Snippet: ytChannelSnippet{
							Thumbnails: ytThumbnails{
								Default: ytThumbnail{URL: "https://yt.com/default.jpg"},
								Medium:  ytThumbnail{URL: "https://yt.com/medium.jpg"},
							},
						},
					}},
				})
			}
		}))
		defer srv.Close()

		adapter := newTestYouTubeAdapter(srv)
		url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Artist2", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://yt.com/medium.jpg" {
			t.Errorf("expected medium thumbnail, got %q", url)
		}
	})
}

func TestYouTubeArtworkResolver_Resolve_NoChannel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ytSearchResponse{Items: []ytSearchItem{}})
	}))
	defer srv.Close()

	adapter := newTestYouTubeAdapter(srv)
	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Unknown", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when no channel found, got %q", url)
	}
}

func TestYouTubeArtworkResolver_Resolve_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	adapter := newTestYouTubeAdapter(srv)
	url, err := adapter.Resolve(context.Background(), domain.ResultKindArtist, "Artist", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on API error, got %q", url)
	}
}

func TestBuildYouTubeQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		title    string
		subtitle string
		want     string
	}{
		{"with subtitle", "Che", "Atlanta rapper", `"Che" "Atlanta rapper"`},
		{"without subtitle", "Che", "", `"Che" music`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildYouTubeQuery(tt.title, tt.subtitle)
			if got != tt.want {
				t.Errorf("buildYouTubeQuery(%q, %q) = %q, want %q", tt.title, tt.subtitle, got, tt.want)
			}
		})
	}
}
