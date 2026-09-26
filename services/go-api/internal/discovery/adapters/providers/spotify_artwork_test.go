package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

import "errors"

import "net/http/httptest"

type fakeRoundTripper struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f.fn(r) }

func oembedClient(status int, thumbnail string, capture *string) *http.Client {
	return &http.Client{Transport: fakeRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		if capture != nil {
			*capture = r.URL.String()
		}
		body := `{"thumbnail_url":"` + thumbnail + `"}`
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}}}
}

func TestSpotifyArtworkResolver_ResolveByIdentity(t *testing.T) {
	t.Run("artist id resolves and upgrades to 640px", func(t *testing.T) {
		var gotURL string
		r := NewSpotifyArtworkResolver(oembedClient(200,
			"https://image-cdn-ak.spotifycdn.com/image/ab67616100005174HASH", &gotURL))

		url, err := r.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"spotify": "SPID123"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(gotURL, "oembed?url=") || !strings.Contains(gotURL, "/artist/SPID123") {
			t.Errorf("request url = %q, want oembed for artist SPID123", gotURL)
		}
		want := "https://image-cdn-ak.spotifycdn.com/image/ab6761610000e5ebHASH"
		if url != want {
			t.Errorf("url = %q, want %q (640px upgrade)", url, want)
		}
	})

	t.Run("no spotify id is a clean miss", func(t *testing.T) {
		r := NewSpotifyArtworkResolver(oembedClient(200, "x", nil))
		url, err := r.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"discogs": "1"}})
		if err != nil || url != "" {
			t.Errorf("got (%q, %v), want clean miss", url, err)
		}
	})

	t.Run("unsupported kind is a clean miss", func(t *testing.T) {
		r := NewSpotifyArtworkResolver(oembedClient(200, "x", nil))
		url, _ := r.ResolveByIdentity(context.Background(), domain.ResultKindPlaylist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"spotify": "SPID123"}})
		if url != "" {
			t.Errorf("url = %q, want miss for unsupported kind", url)
		}
	})

	t.Run("non-200 is a clean miss, not an error", func(t *testing.T) {
		r := NewSpotifyArtworkResolver(oembedClient(404, "x", nil))
		url, err := r.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
			ports.ArtworkIdentity{ExternalIDs: map[string]string{"spotify": "SPID123"}})
		if err != nil || url != "" {
			t.Errorf("got (%q, %v), want clean miss", url, err)
		}
	})
}

func TestSpotifyArtworkResolver_ResolveByIdentity_500IsArtworkUnavailable(t *testing.T) {
	r := NewSpotifyArtworkResolver(oembedClient(500, "x", nil))
	url, err := r.ResolveByIdentity(context.Background(), domain.ResultKindArtist,
		ports.ArtworkIdentity{ExternalIDs: map[string]string{"spotify": "SPID123"}})
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("got (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestSpotifyArtworkResolver_NameResolveIsNoop(t *testing.T) {
	called := false
	r := NewSpotifyArtworkResolver(&http.Client{Transport: fakeRoundTripper{fn: func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	}}})
	url, _ := r.Resolve(context.Background(), domain.ResultKindArtist, "Che", "", "")
	if url != "" || called {
		t.Errorf("Resolve should be a no-op (url=%q called=%v)", url, called)
	}
	if r.ArtworkSource() != "spotify" {
		t.Errorf("ArtworkSource = %q, want spotify", r.ArtworkSource())
	}
}

func spotifyIdentity() ports.ArtworkIdentity {
	return ports.ArtworkIdentity{ExternalIDs: map[string]string{"spotify": "SPID123"}}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_RateLimitIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	url, err := NewSpotifyArtworkResolver(newTestClient(server.URL)).ResolveByIdentity(context.Background(), domain.ResultKindArtist, spotifyIdentity())
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("ResolveByIdentity on HTTP 429 = (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_BadRequestIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	url, err := NewSpotifyArtworkResolver(newTestClient(server.URL)).ResolveByIdentity(context.Background(), domain.ResultKindAlbum, spotifyIdentity())
	if url != "" || err != nil {
		t.Errorf("ResolveByIdentity on HTTP 400 = (%q, %v), want (\"\", nil)", url, err)
	}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_MalformedBodyIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"thumbnail_url":`))
	}))
	defer server.Close()

	url, err := NewSpotifyArtworkResolver(newTestClient(server.URL)).ResolveByIdentity(context.Background(), domain.ResultKindArtist, spotifyIdentity())
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("ResolveByIdentity on a truncated body = (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_EmptyThumbnailIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"title":"Che"}`))
	}))
	defer server.Close()

	url, err := NewSpotifyArtworkResolver(newTestClient(server.URL)).ResolveByIdentity(context.Background(), domain.ResultKindArtist, spotifyIdentity())
	if url != "" || err != nil {
		t.Errorf("ResolveByIdentity with no thumbnail = (%q, %v), want (\"\", nil)", url, err)
	}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_TransportErrorIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	serverURL := server.URL
	server.Close()

	url, err := NewSpotifyArtworkResolver(newTestClient(serverURL)).ResolveByIdentity(context.Background(), domain.ResultKindArtist, spotifyIdentity())
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("ResolveByIdentity against an unreachable host = (%q, %v), want (\"\", ErrArtworkUnavailable)", url, err)
	}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_CancelledContextIsUnavailableAndKeepsTheCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	url, err := NewSpotifyArtworkResolver(newTestClient(server.URL)).ResolveByIdentity(ctx, domain.ResultKindArtist, spotifyIdentity())
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) || !errors.Is(err, context.Canceled) {
		t.Errorf("ResolveByIdentity with a cancelled context = (%q, %v), want (\"\", ErrArtworkUnavailable wrapping context.Canceled)", url, err)
	}
}

func TestSpotifyArtworkResolver_ResolveByIdentity_FailureNamesSpotify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := NewSpotifyArtworkResolver(newTestClient(server.URL)).ResolveByIdentity(context.Background(), domain.ResultKindArtist, spotifyIdentity())
	if err == nil || !strings.Contains(err.Error(), "spotify") {
		t.Errorf("err = %v, want a failure that names spotify", err)
	}
}

func TestChainedArtworkResolver_SpotifyIdentityOn503IsDegradedAndUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	chain := NewChainedArtworkResolver(NewSpotifyArtworkResolver(newTestClient(server.URL)))
	url, _, err := chain.ResolveWithIdentityTagged(context.Background(), domain.ResultKindArtist, "Che", "", spotifyIdentity())
	if url != "" || !errors.Is(err, ports.ErrArtworkDegraded) || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("ResolveWithIdentityTagged on Spotify 503 = (%q, %v), want (\"\", ErrArtworkDegraded and ErrArtworkUnavailable)", url, err)
	}
}
