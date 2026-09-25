package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
	if url != "https://assets.fanart.tv/fanart/music/artist-thumb.jpg" {
		t.Errorf("resolve URL: got %q, want artistthumb URL", url)
	}
}

func TestFanartTvArtworkResolver_Resolve_ArtistBackground_Fallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
	const rgMbid = "01134202-7978-441c-a67b-f5f30c143204"
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"name": "Higher Power",
			"mbid_id": "` + rgMbid + `",
			"albums": {
				"` + rgMbid + `": {
					"albumcover": [
						{"id": "348924", "likes": "0", "url": "https://assets.fanart.tv/fanart/album-cover.jpg"}
					],
					"albumcover_count": 1
				}
			}
		}`))
	}))
	defer server.Close()

	resolver := NewFanartTvArtworkResolver(newTestClient(server.URL), "test-api-key")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindAlbum, "Higher Power", "Coldplay", rgMbid)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if gotPath != "/v3/music/albums/"+rgMbid {
		t.Errorf("album request path: got %q, want the dedicated /v3/music/albums/ endpoint", gotPath)
	}
	if url != "https://assets.fanart.tv/fanart/album-cover.jpg" {
		t.Errorf("resolve URL: got %q, want the nested albumcover URL", url)
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

func TestFanartTvArtworkResolver_EscapesMBIDInRequestURL(t *testing.T) {
	cases := []struct {
		name     string
		kind     domain.ResultKind
		wantPath string
	}{
		{"artist", domain.ResultKindArtist, "/v3/music/a%2Fb%3Fc%23d"},
		{"album", domain.ResultKindAlbum, "/v3/music/albums/a%2Fb%3Fc%23d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got *http.Request
			r := NewFanartTvArtworkResolver(capturingClient(&got), "key-1")
			if _, err := r.Resolve(context.Background(), tc.kind, "X", "Y", hostileMBID); err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got == nil {
				t.Fatal("no request was made")
			}
			if got.URL.Host != "webservice.fanart.tv" {
				t.Errorf("host = %q, want webservice.fanart.tv", got.URL.Host)
			}
			if got.URL.EscapedPath() != tc.wantPath {
				t.Errorf("escaped path = %q, want %q", got.URL.EscapedPath(), tc.wantPath)
			}
			if got.URL.RawQuery != "api_key=key-1" || got.URL.Fragment != "" {
				t.Errorf("query = %q fragment = %q, want api_key=key-1 and no fragment",
					got.URL.RawQuery, got.URL.Fragment)
			}
		})
	}
}
