package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// timingOutContentProvider fails every content call the way an HTTP adapter
// does when its client deadline passes: a *url.Error wrapping the deadline.
type timingOutContentProvider struct{}

func (timingOutContentProvider) fail() ([]discdomain.SearchResult, error) {
	return nil, &url.Error{Op: "Get", URL: "https://upstream.example/x", Err: context.DeadlineExceeded}
}

func (p timingOutContentProvider) GetAlbumTracks(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetArtistTopTracks(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetArtistAlbums(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetRelatedTracks(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	return p.fail()
}

// errorContractRouter serves the discovery routes over real content services:
// itunes times out, soundcloud answers but its circuit is already open, and
// spotify has no adapter wired at all.
func errorContractRouter(t *testing.T) chi.Router {
	t.Helper()
	breaker := service.NewCircuitBreaker()
	for i := 0; breaker.AllowRequest(discdomain.ProviderSoundCloud); i++ {
		if i > 100 {
			t.Fatal("circuit breaker never opened for soundcloud")
		}
		breaker.RecordFailure(discdomain.ProviderSoundCloud)
	}
	timingOut := timingOutContentProvider{}
	healthy := stubContentProvider{provider: discdomain.ProviderSoundCloud}
	h := NewDiscoveryHandler(DiscoveryServices{
		Album: service.NewGetAlbumTracksService(map[discdomain.ProviderName]ports.AlbumContentProvider{
			discdomain.ProviderITunes: timingOut, discdomain.ProviderSoundCloud: healthy,
		}, service.WithAlbumCircuitBreaker(breaker)),
		Artist: service.NewGetArtistContentService(map[discdomain.ProviderName]ports.ArtistContentProvider{
			discdomain.ProviderITunes: timingOut, discdomain.ProviderSoundCloud: healthy,
		}, service.WithContentCircuitBreaker(breaker)),
		Related: service.NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{
			discdomain.ProviderITunes.String(): timingOut, discdomain.ProviderSoundCloud.String(): healthy,
		}, service.WithRelatedCircuitBreaker(breaker)),
	})
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())
	return router
}

// contentFailure is everything a caller can observe about a failed fetch.
type contentFailure struct {
	HTTP   int
	Status string
	Code   string
}

func (f contentFailure) String() string {
	return fmt.Sprintf("HTTP %d status=%q code=%q", f.HTTP, f.Status, f.Code)
}

var contentErrorCauses = []struct {
	cause    string
	provider string
	want     contentFailure
}{
	{
		cause: "upstream timeout", provider: "itunes",
		want: contentFailure{HTTP: http.StatusGatewayTimeout, Status: "timeout", Code: "discovery.provider_timeout"},
	},
	{
		cause: "circuit open", provider: "soundcloud",
		want: contentFailure{HTTP: http.StatusServiceUnavailable, Status: "circuit_open", Code: "discovery.provider_circuit_open"},
	},
	{
		cause: "provider not configured", provider: "spotify",
		want: contentFailure{HTTP: http.StatusNotFound, Status: "error", Code: "discovery.content_unserved"},
	},
}

var contentErrorEndpoints = []struct {
	name     string
	path     string
	combined bool
}{
	{name: "album tracks", path: "/discovery/albums/%s/id-1/tracks"},
	{name: "artist top tracks", path: "/discovery/artists/%s/id-1/top-tracks"},
	{name: "artist albums", path: "/discovery/artists/%s/id-1/albums"},
	{name: "related tracks", path: "/discovery/tracks/%s/id-1/related"},
	{name: "artist content", path: "/discovery/artists/%s/id-1/content", combined: true},
}

func decodeContentFailure(t *testing.T, httpCode int, raw json.RawMessage) contentFailure {
	t.Helper()
	var body struct {
		Status string            `json:"status"`
		Code   string            `json:"code"`
		Items  []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if body.Items == nil || len(body.Items) != 0 {
		t.Errorf("failed fetch items = %s, want an empty array", raw)
	}
	return contentFailure{HTTP: httpCode, Status: body.Status, Code: body.Code}
}

// The combined artist content response fails only when both halves failed, so
// a response carrying either half's content stays 200.
func TestArtistContentOutcome(t *testing.T) {
	ok := &service.ContentFetchResponse{Status: discdomain.ProviderStatusOK}
	partial := &service.ContentFetchResponse{Status: discdomain.ProviderStatusOK, Partial: true}
	timeout := &service.ContentFetchResponse{Status: discdomain.ProviderStatusTimeout}
	broken := &service.ContentFetchResponse{Status: discdomain.ProviderStatusError}
	throttled := &service.ContentFetchResponse{Status: discdomain.ProviderStatusRateLimited}
	unserved := &service.ContentFetchResponse{Status: discdomain.ProviderStatusError, Unserved: true}
	cases := []struct {
		name           string
		tracks, albums *service.ContentFetchResponse
		wantHTTP       int
		wantCode       string
	}{
		{name: "both ok", tracks: ok, albums: ok, wantHTTP: http.StatusOK},
		{name: "partial halves", tracks: partial, albums: partial, wantHTTP: http.StatusOK},
		{name: "only tracks failed", tracks: timeout, albums: ok, wantHTTP: http.StatusOK},
		{name: "only albums failed", tracks: ok, albums: broken, wantHTTP: http.StatusOK},
		{name: "both failed, tracks decide", tracks: throttled, albums: broken, wantHTTP: http.StatusServiceUnavailable, wantCode: contentCodeRateLimited},
		{name: "called failure outranks unserved", tracks: unserved, albums: broken, wantHTTP: http.StatusBadGateway, wantCode: contentCodeProviderError},
		{name: "both unserved", tracks: unserved, albums: unserved, wantHTTP: http.StatusNotFound, wantCode: contentCodeUnserved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotHTTP, gotCode := artistContentOutcome(tc.tracks, tc.albums)
			if gotHTTP != tc.wantHTTP || gotCode != tc.wantCode {
				t.Errorf("outcome = %d %q, want %d %q", gotHTTP, gotCode, tc.wantHTTP, tc.wantCode)
			}
		})
	}
}

// TestContentFetchEndpoints_DistinguishFailureCauses drives each content-fetch
// endpoint into a timeout, an open circuit and an unconfigured provider, and
// requires each cause to be told apart by HTTP status, provider status and
// error code, instead of collapsing into one 200 {"status":"error"}.
func TestContentFetchEndpoints_DistinguishFailureCauses(t *testing.T) {
	for _, ep := range contentErrorEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			router := errorContractRouter(t)
			seen := map[contentFailure]string{}
			for _, c := range contentErrorCauses {
				rec := discServe(t, router, http.MethodGet, fmt.Sprintf(ep.path, c.provider), nil)

				var parts []json.RawMessage
				if ep.combined {
					var combined struct {
						Code      string          `json:"code"`
						TopTracks json.RawMessage `json:"top_tracks"`
						Albums    json.RawMessage `json:"albums"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &combined); err != nil {
						t.Fatalf("%s: decode %s: %v", c.cause, rec.Body.String(), err)
					}
					if combined.Code != c.want.Code {
						t.Errorf("%s: top-level code = %q, want %q", c.cause, combined.Code, c.want.Code)
					}
					parts = []json.RawMessage{combined.TopTracks, combined.Albums}
				} else {
					parts = []json.RawMessage{rec.Body.Bytes()}
				}

				for _, raw := range parts {
					got := decodeContentFailure(t, rec.Code, raw)
					if got != c.want {
						t.Errorf("%s: got %s, want %s (body %s)", c.cause, got, c.want, strings.TrimSpace(rec.Body.String()))
					}
					if other, dup := seen[got]; dup && other != c.cause {
						t.Errorf("%s is indistinguishable from %s: both %s", c.cause, other, got)
					}
					seen[got] = c.cause
				}
			}
		})
	}
}

type stubContentProvider struct {
	provider discdomain.ProviderName
	down     bool
}

func (p stubContentProvider) answer(kind discdomain.ResultKind, id string) ([]discdomain.SearchResult, error) {
	if p.down {
		return nil, errors.New("upstream unavailable")
	}
	return []discdomain.SearchResult{{
		Kind:     kind,
		Title:    "Real Song",
		Subtitle: "Artist",
		Sources:  []discdomain.SourceRef{{Provider: p.provider, ExternalID: id}},
		Extras:   map[string]any{},
	}}, nil
}

func (p stubContentProvider) GetAlbumTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindTrack, id)
}

func (p stubContentProvider) GetArtistTopTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindTrack, id)
}

func (p stubContentProvider) GetArtistAlbums(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindAlbum, id)
}

func (p stubContentProvider) GetRelatedTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindTrack, id)
}
