package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingRoundTripper struct{ called bool }

func (r *recordingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.called = true
	return nil, errors.New("network call should not happen")
}

type cannedRoundTripper struct {
	status int
	body   string
	calls  int
}

func (rt *cannedRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	rt.calls++
	return &http.Response{
		StatusCode: rt.status,
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Header:     make(http.Header),
	}, nil
}

func TestYTMSearchRetry_RespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rt := &recordingRoundTripper{}
	client := &http.Client{Transport: rt}
	_, err := ytmSearchRetry(ctx, client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("want a context error, got nil")
	}
	if rt.called {
		t.Error("must not start a network call when the context is already cancelled")
	}
}

func TestYTMSearch_ThrottledResponseSurfacesStatusErrorForTheBreaker(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusTooManyRequests, body: `{"error":{"code":429,"message":"rate limited"}}`}
	client := &http.Client{Transport: rt}

	_, err := ytmSearch(context.Background(), client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("a 429 whose body parses returned nil; the breaker would record a healthy empty answer")
	}
	var status httpStatusCoder
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want an error carrying HTTPStatus() the breaker classifies as a failure", err)
	}
	if got := status.HTTPStatus(); got != http.StatusTooManyRequests {
		t.Errorf("HTTPStatus() = %d, want 429", got)
	}
}

func TestYTMSearchRetry_DoesNotRetryPermanent4xx(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusNotFound, body: `{}`}
	client := &http.Client{Transport: rt}

	_, err := ytmSearchRetry(context.Background(), client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("want a status-bearing error on a 404")
	}
	if rt.calls != 1 {
		t.Errorf("calls = %d, want 1: a permanent 404 must not be reattempted", rt.calls)
	}
}

func TestYTMSearchRetry_RetriesTransient5xx(t *testing.T) {
	rt := &cannedRoundTripper{status: http.StatusServiceUnavailable, body: `{}`}
	client := &http.Client{Transport: rt}

	_, err := ytmSearchRetry(context.Background(), client, "anything", ytmNoFilter)

	if err == nil {
		t.Fatal("want a status-bearing error on a 503")
	}
	if rt.calls != 2 {
		t.Errorf("calls = %d, want 2: a 503 is transient and must be retried once", rt.calls)
	}
}

func TestResizeYTThumbnail(t *testing.T) {
	tests := []struct {
		name string
		url  string
		size int
		want string
	}{
		{
			name: "album thumbnail without crop flag",
			url:  "https://yt3.googleusercontent.com/abc=w544-h544-l90-rj",
			size: 1000,
			want: "https://yt3.googleusercontent.com/abc=w1000-h1000-l90-rj",
		},
		{
			name: "artist thumbnail preserves the -p- smart-crop flag",
			url:  "https://lh3.googleusercontent.com/xyz=w120-h120-p-l90-rj",
			size: 1000,
			want: "https://lh3.googleusercontent.com/xyz=w1000-h1000-p-l90-rj",
		},
		{
			name: "unrecognized size segment returns the url unchanged",
			url:  "https://img/no-size-here",
			size: 1000,
			want: "https://img/no-size-here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resizeYTThumbnail(tt.url, tt.size); got != tt.want {
				t.Errorf("resizeYTThumbnail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPickArtistArtwork(t *testing.T) {
	thumb := func(url string) []ytmThumbnail {
		return []ytmThumbnail{{URL: "https://img/small=w60-h60-rj"}, {URL: url}}
	}

	t.Run("prefers an exact case-insensitive name match over the top result", func(t *testing.T) {
		artists := []*ytmArtistItem{
			{Artist: "Kendrick Lamar Type Beat", Thumbnails: thumb("https://img/wrong=w120-h120-rj")},
			{Artist: "kendrick lamar", Thumbnails: thumb("https://img/right=w120-h120-p-rj")},
		}
		got := pickArtistArtwork(artists, "Kendrick Lamar", 1000)
		want := "https://img/right=w1000-h1000-p-rj"
		if got != want {
			t.Errorf("got %q, want the exact-match artist resized to 1000: %q", got, want)
		}
	})

	t.Run("falls back to the top result when no exact match", func(t *testing.T) {
		artists := []*ytmArtistItem{
			{Artist: "Some Other Artist", Thumbnails: thumb("https://img/top=w120-h120-rj")},
		}
		got := pickArtistArtwork(artists, "Nonexistent", 1000)
		want := "https://img/top=w1000-h1000-rj"
		if got != want {
			t.Errorf("got %q, want the top result resized: %q", got, want)
		}
	})

	t.Run("returns empty when no artist carries a thumbnail", func(t *testing.T) {
		artists := []*ytmArtistItem{{Artist: "No Image", Thumbnails: nil}}
		if got := pickArtistArtwork(artists, "No Image", 1000); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns empty for no artists", func(t *testing.T) {
		if got := pickArtistArtwork(nil, "Anyone", 1000); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestYouTubeMusicArtworkResolver_NonArtistKindIsNoop(t *testing.T) {
	r := NewYouTubeMusicArtworkResolver(nil)
	for _, kind := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindAlbum} {
		got, err := r.Resolve(context.Background(), kind, "Some Title", "Some Subtitle", "")
		if err != nil {
			t.Errorf("kind %v: unexpected error %v", kind, err)
		}
		if got != "" {
			t.Errorf("kind %v: got %q, want empty (artist-only resolver)", kind, got)
		}
	}
}

func TestMapYTMusicVideo(t *testing.T) {
	v := &ytmVideo{
		VideoID:  "abc123",
		Title:    "Obscure Track",
		Artists:  []ytmArtistRef{{Name: "Underground Artist"}},
		Duration: 200,
		Thumbnails: []ytmThumbnail{
			{URL: "https://img/small"},
			{URL: "https://img/large"},
		},
	}

	got := mapYTMusicVideo(v)

	if got.Kind != domain.ResultKindTrack {
		t.Errorf("Kind = %v, want track", got.Kind)
	}
	if got.Title != "Obscure Track" {
		t.Errorf("Title = %q, want %q", got.Title, "Obscure Track")
	}
	if got.Subtitle != "Underground Artist" {
		t.Errorf("Subtitle = %q, want %q", got.Subtitle, "Underground Artist")
	}
	if got.ImageURL != "https://img/large" {
		t.Errorf("ImageURL = %q, want the largest thumbnail", got.ImageURL)
	}
	if len(got.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1", len(got.Sources))
	}
	src := got.Sources[0]
	if src.Provider != domain.ProviderYouTube || src.ExternalID != "abc123" {
		t.Errorf("source = %+v, want youtube/abc123", src)
	}
	if src.URL != "https://music.youtube.com/watch?v=abc123" {
		t.Errorf("source URL = %q", src.URL)
	}
	if got.Extras["duration"] != 200 {
		t.Errorf("duration extra = %v, want 200", got.Extras["duration"])
	}
}
