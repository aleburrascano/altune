package handler_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type timingOutContentProvider struct{}

func (timingOutContentProvider) fail() ([]discovery.SearchResult, error) {
	return nil, &url.Error{Op: "Get", URL: "https://upstream.example/x", Err: context.DeadlineExceeded}
}

func (p timingOutContentProvider) GetAlbumTracks(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetArtistTopTracks(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetArtistAlbums(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetRelatedTracks(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	return p.fail()
}

func errorContractRouter(t *testing.T) chi.Router {
	t.Helper()
	breaker := discovery.NewCircuitBreaker()
	for i := 0; breaker.AllowRequest(context.Background(), discovery.ProviderSoundCloud); i++ {
		if i > 100 {
			t.Fatal("circuit breaker never opened for soundcloud")
		}
		breaker.RecordFailure(context.Background(), discovery.ProviderSoundCloud)
	}
	timingOut := timingOutContentProvider{}
	healthy := stubContentProvider{provider: discovery.ProviderSoundCloud}
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{
		Album: discovery.NewGetAlbumTracksService(map[discovery.ProviderName]discovery.AlbumContentProvider{
			discovery.ProviderITunes: timingOut, discovery.ProviderSoundCloud: healthy,
		}, discovery.WithAlbumCircuitBreaker(breaker)),
		Artist: discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderITunes: timingOut, discovery.ProviderSoundCloud: healthy,
		}, discovery.WithContentCircuitBreaker(breaker)),
		Related: discovery.NewGetRelatedTracksService(map[string]discovery.RelatedTracksProvider{
			discovery.ProviderITunes.String(): timingOut, discovery.ProviderSoundCloud.String(): healthy,
		}, discovery.WithRelatedCircuitBreaker(breaker)),
	})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", h.Routes())
	return router
}

type contentFailure struct {
	HTTP int
	Code string
}

func (f contentFailure) String() string {
	return fmt.Sprintf("HTTP %d code=%q", f.HTTP, f.Code)
}

var contentErrorCauses = []struct {
	cause    string
	provider string
	want     contentFailure
}{
	{
		cause: "upstream timeout", provider: "itunes",
		want: contentFailure{HTTP: http.StatusGatewayTimeout, Code: "discovery.provider_timeout"},
	},
	{
		cause: "circuit open", provider: "soundcloud",
		want: contentFailure{HTTP: http.StatusServiceUnavailable, Code: "discovery.provider_circuit_open"},
	},
	{
		cause: "provider not configured", provider: "spotify",
		want: contentFailure{HTTP: http.StatusNotFound, Code: "discovery.content_unserved"},
	},
}

var contentErrorEndpoints = []struct {
	name string
	path string
}{
	{name: "album tracks", path: "/discovery/albums/%s/id-1/tracks"},
	{name: "artist top tracks", path: "/discovery/artists/%s/id-1/top-tracks"},
	{name: "artist albums", path: "/discovery/artists/%s/id-1/albums"},
	{name: "related tracks", path: "/discovery/tracks/%s/id-1/related"},
	{name: "artist content", path: "/discovery/artists/%s/id-1/content"},
}

func decodeContentFailure(t *testing.T, rec *httptest.ResponseRecorder) contentFailure {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("failed fetch Content-Type = %q, want application/problem+json", got)
	}
	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if problem.Status != rec.Code {
		t.Errorf("problem status = %d, want the response status %d", problem.Status, rec.Code)
	}
	return contentFailure{HTTP: rec.Code, Code: problem.Code}
}

type throttledError struct{}

func (throttledError) Error() string { return "upstream said 429" }

func (throttledError) UpstreamStatus() int { return http.StatusTooManyRequests }

type scriptedArtistProvider struct {
	tracksErr, albumsErr error
}

func (p scriptedArtistProvider) answer(kind discovery.ResultKind, id string, err error) ([]discovery.SearchResult, error) {
	var partial *discovery.PartialResultError
	if err != nil && !errors.As(err, &partial) {
		return nil, err
	}
	return []discovery.SearchResult{{
		Kind:    kind,
		Title:   "Real Song",
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: id}},
		Extras:  map[string]any{},
	}}, err
}

func (p scriptedArtistProvider) GetArtistTopTracks(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	return p.answer(discovery.ResultKindTrack, id, p.tracksErr)
}

func (p scriptedArtistProvider) GetArtistAlbums(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	return p.answer(discovery.ResultKindAlbum, id, p.albumsErr)
}

func TestArtistContent_OutcomeFollowsBothHalves(t *testing.T) {
	partial := &discovery.PartialResultError{Page: 2, Err: errors.New("page 2 failed")}
	timeout := &url.Error{Op: "Get", URL: "https://upstream.example/x", Err: context.DeadlineExceeded}
	broken := errors.New("upstream broke")
	cases := []struct {
		name                 string
		tracksErr, albumsErr error
		provider             string
		wantHTTP             int
		wantCode             string
	}{
		{name: "both ok", provider: "deezer", wantHTTP: http.StatusOK},
		{name: "partial halves", tracksErr: partial, albumsErr: partial, provider: "deezer", wantHTTP: http.StatusOK},
		{name: "only tracks failed", tracksErr: timeout, provider: "deezer", wantHTTP: http.StatusOK},
		{name: "only albums failed", albumsErr: broken, provider: "deezer", wantHTTP: http.StatusOK},
		{
			name: "both failed, tracks decide", tracksErr: throttledError{}, albumsErr: broken, provider: "deezer",
			wantHTTP: http.StatusServiceUnavailable, wantCode: "discovery.provider_rate_limited",
		},
		{
			name: "both failed, albums cause ignored", tracksErr: broken, albumsErr: throttledError{}, provider: "deezer",
			wantHTTP: http.StatusBadGateway, wantCode: "discovery.provider_error",
		},
		{name: "both unserved", provider: "spotify", wantHTTP: http.StatusNotFound, wantCode: "discovery.content_unserved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{
				Artist: discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{
					discovery.ProviderDeezer: scriptedArtistProvider{tracksErr: tc.tracksErr, albumsErr: tc.albumsErr},
				}),
			})
			router := chi.NewRouter()
			router.Use(discAsTestUser)
			router.Mount("/discovery", h.Routes())

			rec := discServe(t, router, http.MethodGet, "/discovery/artists/"+tc.provider+"/id-1/content", nil)
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s: %v", rec.Body.String(), err)
			}
			if rec.Code != tc.wantHTTP || body.Code != tc.wantCode {
				t.Errorf("outcome = %d %q, want %d %q (body %s)", rec.Code, body.Code, tc.wantHTTP, tc.wantCode, rec.Body.String())
			}
		})
	}
}

func TestContentFetchEndpoints_DistinguishFailureCauses(t *testing.T) {
	for _, ep := range contentErrorEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			router := errorContractRouter(t)
			seen := map[contentFailure]string{}
			for _, c := range contentErrorCauses {
				rec := discServe(t, router, http.MethodGet, fmt.Sprintf(ep.path, c.provider), nil)

				got := decodeContentFailure(t, rec)
				if got != c.want {
					t.Errorf("%s: got %s, want %s (body %s)", c.cause, got, c.want, strings.TrimSpace(rec.Body.String()))
				}
				if other, dup := seen[got]; dup && other != c.cause {
					t.Errorf("%s is indistinguishable from %s: both %s", c.cause, other, got)
				}
				seen[got] = c.cause
			}
		})
	}
}
