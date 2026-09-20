package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/discovery/domain"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	adminHandler "altune/go-api/internal/admin/handler"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

const (
	leakLastFMKey     = "0123456789abcdef0123456789abcdef"
	leakSoundCloudCID = "ScrapedClientIdValue0123456789AB"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func assertNoProviderSecret(t *testing.T, where, s string) {
	t.Helper()
	for _, secret := range []string{leakLastFMKey, leakSoundCloudCID} {
		if strings.Contains(s, secret) {
			t.Errorf("%s leaks provider secret %q:\n%s", where, secret, s)
		}
	}
}

// serveDetail runs POST /rerun-detail through the real admin handler so the
// assertion sees the exact JSON an operator receives.
func serveDetail(t *testing.T, runner adminHandler.DetailReRunner, query string) string {
	t.Helper()
	r := chi.NewRouter()
	adminHandler.New(nil, nil).WithDetailReRunner(runner).RegisterData(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rerun-detail", strings.NewReader(`{"query":"`+query+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestLogSeedError_surfacesProviderFetchError reproduces the gap: when a seed
// fetch fails, seedFrom produced a bare rawSeed{status:"error"} and discarded
// the underlying error entirely — never logged and never carried. ReRunDetail
// is an operator diagnostic for "why doesn't X show up", so the reason (bad
// provider id, timeout, 404) must be surfaced the way sibling fanOutRerun
// already captures it into ProviderTrace.Err, not silently dropped. The log
// runs through a JSON handler so the typed provider must render by name.
func TestLogSeedError_surfacesProviderFetchError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	seed := logSeedError(context.Background(), seedFrom(domain.ProviderDeezer, "abc123", nil, errors.New("upstream 404")))

	if seed.status != domain.ProviderStatusError {
		t.Fatalf("want error status for a failed fetch, got %q", seed.status)
	}
	if seed.err == "" {
		t.Errorf("provider fetch error was silently dropped: rawSeed carried no error message")
	}
	logged := buf.String()
	for _, want := range []string{`"external_id":"abc123"`, `"provider":"deezer"`, "upstream 404"} {
		if !strings.Contains(logged, want) {
			t.Errorf("seed fetch error not surfaced at the call site: want %q in log=%q", want, logged)
		}
	}
}

// TestProjectSeeds_surfacesErrorToOperator guards the wire surface: an errored
// seed must carry its reason into the admin JSON "error" field so the operator
// can distinguish a timeout from a 404 from a bad provider id — but a
// transport failure's *url.Error embeds the full request URL, so the LastFM
// api_key and SoundCloud client_id values must arrive as REDACTED, both there
// and in the rerun_detail.seed_fetch_failed log event.
func TestProjectSeeds_surfacesErrorToOperator(t *testing.T) {
	cases := []struct {
		provider     domain.ProviderName
		rawURL, want string
	}{
		{domain.ProviderLastFM, "https://ws.audioscrobbler.com/2.0/?method=artist.gettoptracks&api_key=" + leakLastFMKey + "&format=json", "api_key=REDACTED"},
		{domain.ProviderSoundCloud, "https://api-v2.soundcloud.com/users/1/toptracks?client_id=" + leakSoundCloudCID + "&limit=50", "client_id=REDACTED"},
	}
	for _, tc := range cases {
		t.Run(tc.provider.String(), func(t *testing.T) {
			logs := captureSlog(t)
			fetchErr := &url.Error{Op: "Get", URL: tc.rawURL, Err: errors.New("dial tcp: connection refused")}

			seed := logSeedError(context.Background(), seedFrom(tc.provider, "id-1", nil, fetchErr))
			body := serveDetail(t, func(context.Context, string) (requeststore.DetailReRunResult, error) {
				return requeststore.DetailReRunResult{TrackSeeds: projectSeeds([]rawSeed{seed})}, nil
			}, "x")

			for _, want := range []string{tc.want, "connection refused"} {
				if !strings.Contains(body, want) {
					t.Errorf("admin JSON error field missing %q: %s", want, body)
				}
			}
			logged := logs.String()
			if !strings.Contains(logged, "rerun_detail.seed_fetch_failed") || !strings.Contains(logged, tc.want) {
				t.Errorf("seed_fetch_failed event missing redacted %q:\n%s", tc.want, logged)
			}
			assertNoProviderSecret(t, "admin JSON response", body)
			assertNoProviderSecret(t, "seed_fetch_failed log", logged)
		})
	}
}

type emptyIdentityStore struct{}

func (emptyIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (emptyIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[string]string, bool) {
	return "", nil, false
}

func (emptyIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

type leakRoundTripper func(*http.Request) (*http.Response, error)

func (f leakRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func textResponse(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

// sentURLs records what the fan-out actually put on the wire. The fan-out is
// concurrent, so the recording is guarded.
type sentURLs struct {
	mu   sync.Mutex
	urls []string
}

func (s *sentURLs) add(rawURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = append(s.urls, rawURL)
}

func (s *sentURLs) carried(secret string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.urls {
		if strings.Contains(u, secret) {
			return true
		}
	}
	return false
}

// failingProviderClient serves SoundCloud's home page and asset bundle so the
// real adapter scrapes a client_id, then fails every LastFM and SoundCloud
// api-v2 call at the transport. net/http wraps those failures in *url.Error,
// whose Error() embeds the full request URL with api_key / client_id.
func failingProviderClient(sent *sentURLs) *http.Client {
	return &http.Client{Transport: leakRoundTripper(func(r *http.Request) (*http.Response, error) {
		sent.add(r.URL.String())
		switch {
		case r.URL.Host == "soundcloud.com":
			return textResponse(r, `<script src="https://a-v2.sndcdn.com/assets/app-1.js"></script>`), nil
		case strings.HasSuffix(r.URL.Path, "/assets/app-1.js"):
			return textResponse(r, `x={client_id:"`+leakSoundCloudCID+`"}`), nil
		}
		return nil, errors.New("dial tcp: connection refused")
	})}
}

// TestReRunDetail_networkFailureDoesNotLeakProviderSecrets drives the real
// /rerun-detail handler through reRunDetail with the real LastFM and SoundCloud
// adapters while their upstream calls fail at the transport. Neither the admin
// JSON response nor any log line emitted during the run may carry the LastFM
// api_key or the scraped SoundCloud client_id.
func TestReRunDetail_networkFailureDoesNotLeakProviderSecrets(t *testing.T) {
	logs := captureSlog(t)

	sent := &sentURLs{}
	client := failingProviderClient(sent)
	artist := domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      "Leaky Artist",
		MBID:       "5b11f4ce-a62d-471e-81fc-a69a8278c7da",
		Confidence: domain.ConfidenceHigh,
		Popularity: 80,
		FanCount:   100000,
		ImageURL:   "https://img.test/leaky.jpg",
		Sources: []domain.SourceRef{
			{Provider: domain.ProviderDeezer, ExternalID: "9"},
			{Provider: domain.ProviderSoundCloud, ExternalID: "123456"},
		},
	}
	soundcloud := providers.NewSoundCloudAPIAdapter(client, nil)
	lastfm := providers.NewLastFmAdapter(client, leakLastFMKey)
	// The real adapters also sit in the search fan-out that resolves the
	// artist, so its provider_failed events are covered too.
	searchSvc := discoveryService.NewService([]discoveryPorts.SearchProvider{
		outageProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{artist}},
		soundcloud,
		lastfm,
	}, discoveryService.NewCircuitBreaker())
	// A store (even an empty one) routes content fetches through the
	// identity fan-out first, as production wiring does.
	artistSvc := discoveryService.NewGetArtistContentService(
		map[domain.ProviderName]discoveryPorts.ArtistContentProvider{
			domain.ProviderSoundCloud: soundcloud,
			domain.ProviderLastFM:     lastfm,
		},
		discoveryService.WithContentIdentityStore(emptyIdentityStore{}),
	)

	body := serveDetail(t, func(ctx context.Context, query string) (requeststore.DetailReRunResult, error) {
		return reRunDetail(ctx, searchSvc, artistSvc, inspectorBudget, query)
	}, "Leaky Artist")

	if !strings.Contains(body, `"provider":"soundcloud"`) || !strings.Contains(body, `"provider":"lastfm"`) {
		t.Fatalf("fan-out did not reach both soundcloud and lastfm seeds: %s", body)
	}
	assertNoProviderSecret(t, "admin JSON response", body)

	// Prove both secrets really were in flight, so the no-leak assertion is not
	// vacuous. The wire is where that holds now: providerhttp strips the query
	// from a transport failure, so the secret-bearing URL never reaches a log
	// line to be found redacted there.
	for _, secret := range []string{leakLastFMKey, leakSoundCloudCID} {
		if !sent.carried(secret) {
			t.Errorf("no request carried %q, so the no-leak assertion proves nothing", secret)
		}
	}

	logged := logs.String()
	for _, want := range []string{
		"ws.audioscrobbler.com", "api-v2.soundcloud.com",
		"search.v2.provider_failed", "artist_content.fanout.provider_failed",
		"artist_top_tracks.provider_failed", "artist_albums.provider_failed",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("expected redacted %q in logs of the failed fetch:\n%s", want, logged)
		}
	}
	assertNoProviderSecret(t, "logs", logged)
}
