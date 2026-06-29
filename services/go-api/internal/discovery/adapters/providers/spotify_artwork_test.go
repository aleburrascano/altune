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

func TestSpotifyArtworkResolver_NameResolveIsNoop(t *testing.T) {
	// Identity-only: a name resolve must never call out (would risk a wrong face).
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
