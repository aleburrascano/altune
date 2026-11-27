package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type timingOutContentProvider struct{}

func (timingOutContentProvider) fail() ([]search.SearchResult, error) {
	return nil, &url.Error{Op: "Get", URL: "https://upstream.example/x", Err: context.DeadlineExceeded}
}

func (p timingOutContentProvider) GetAlbumTracks(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetArtistTopTracks(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetArtistAlbums(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	return p.fail()
}

func (p timingOutContentProvider) GetRelatedTracks(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	return p.fail()
}

func errorContractRouter(t *testing.T) chi.Router {
	t.Helper()
	breaker := search.NewCircuitBreaker(clock.Now)
	for i := 0; ; i++ {
		adm, ok := breaker.AllowRequest(context.Background(), search.ProviderSoundCloud)
		if !ok {
			break
		}
		if i > 100 {
			t.Fatal("circuit breaker never opened for soundcloud")
		}
		breaker.RecordFailure(context.Background(), search.ProviderSoundCloud, adm)
	}
	timingOut := timingOutContentProvider{}
	healthy := stubContentProvider{provider: search.ProviderSoundCloud}
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Album: searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{
			domain.ProviderITunes: timingOut, search.ProviderSoundCloud: healthy,
		}, searchtest.AlbumBreaker(breaker)),
		Artist: searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{
			domain.ProviderITunes: timingOut, search.ProviderSoundCloud: healthy,
		}, searchtest.ArtistBreaker(breaker)),
		Related: searchtest.NewRelatedTracksWithBreaker(t, map[string]ports.RelatedTracksProvider{
			domain.ProviderITunes.String(): timingOut, search.ProviderSoundCloud.String(): healthy,
		}, breaker),
	}), time.Now)
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

func (p scriptedArtistProvider) answer(kind search.ResultKind, id string, err error) ([]search.SearchResult, error) {
	var partial *domain.PartialResultError
	if err != nil && !errors.As(err, &partial) {
		return nil, err
	}
	return []search.SearchResult{memory.NewResult(
		kind,
		"Real Song",
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: id}}),
		memory.WithExtras(map[string]any{}),
	)}, err
}

func (p scriptedArtistProvider) GetArtistTopTracks(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
	return p.answer(search.ResultKindTrack, id, p.tracksErr)
}

func (p scriptedArtistProvider) GetArtistAlbums(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
	return p.answer(search.ResultKindAlbum, id, p.albumsErr)
}

func TestArtistContent_OutcomeFollowsBothHalves(t *testing.T) {
	partial := &domain.PartialResultError{Page: 2, Err: errors.New("page 2 failed")}
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
			h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
				Artist: searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{
					search.ProviderDeezer: scriptedArtistProvider{tracksErr: tc.tracksErr, albumsErr: tc.albumsErr},
				}),
			}), time.Now)
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
