package providertest

import (
	"altune/go-api/internal/discovery/domain"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type RedirectTransport struct {
	TargetURL string
}

func (t *RedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.TargetURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

func NewClient(serverURL string) *http.Client {
	return &http.Client{Transport: &RedirectTransport{TargetURL: serverURL}}
}

func ArtworkFailureHandlers() map[string]http.HandlerFunc {
	handlers := artworkStatusFailureHandlers()
	for name, h := range artworkBodyFailureHandlers() {
		handlers[name] = h
	}
	return handlers
}

func artworkStatusFailureHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"429 with Retry-After": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		"503": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		},
		"redirect loop": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
		},
		"302 without a Location": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusFound)
		},
	}
}

func artworkBodyFailureHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"200 with truncated JSON": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"hits":[{"result":`))
		},
		"200 with an HTML body": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html>captcha</html>`))
		},
		"200 with well-formed JSON of the wrong shape": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["not","an","object"]`))
		},
	}
}

func ArtworkVerifiedMissHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"404": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
		"400": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		},
		"redirect to a 404": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/gone") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			http.Redirect(w, r, "/gone", http.StatusMovedPermanently)
		},
	}
}

func CancelMidRequestServer(t *testing.T) (*httptest.Server, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("client kept the request open after its context was cancelled")
		}
	}))
	t.Cleanup(func() {
		srv.Close()
		cancel()
	})
	return srv, ctx
}

func NewNoFollowClient(serverURL string) *http.Client {
	return &http.Client{
		Transport: &RedirectTransport{TargetURL: serverURL},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type RewriteTransport struct {
	Base string
}

func (t *RewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.Base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req)
}

type FakeRoundTripper struct {
	Fn func(*http.Request) (*http.Response, error)
}

func (f FakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f.Fn(r) }

var FarFuture = time.Now().Add(24 * time.Hour)

const HostileMBID = "a/b?c#d"

func CapturingClient(capture **http.Request) *http.Client {
	return &http.Client{Transport: FakeRoundTripper{Fn: func(r *http.Request) (*http.Response, error) {
		*capture = r
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	}}}
}

const SensitiveQuery = "zqxj private diagnosis clinic"

func CaptureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func AssertNoQueryText(t *testing.T, logged string) {
	t.Helper()
	for _, form := range []string{SensitiveQuery, url.QueryEscape(SensitiveQuery), url.PathEscape(SensitiveQuery), "diagnosis"} {
		if strings.Contains(logged, form) {
			t.Fatalf("log output contains search text %q:\n%s", form, logged)
		}
	}
}

func AllKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func TrackKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

func AlbumKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindAlbum: true}
}

func ArtistKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindArtist: true}
}

type BurstResult struct {
	Err     error
	Elapsed time.Duration
}

func FireBurst(t *testing.T, wait func(context.Context) error, n int, window time.Duration) []BurstResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	results := make(chan BurstResult, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := wait(ctx)
			results <- BurstResult{Err: err, Elapsed: time.Since(start)}
		}()
	}
	done := collectBurst(results, n, window)
	cancel()
	wg.Wait()
	return done
}

func collectBurst(results <-chan BurstResult, n int, window time.Duration) []BurstResult {
	timer := time.NewTimer(window)
	defer timer.Stop()
	var done []BurstResult
	for len(done) < n {
		select {
		case r := <-results:
			done = append(done, r)
		case <-timer.C:
			return done
		}
	}
	return done
}

type Contract struct {
	Name     string
	Golden   string
	Search   func(t *testing.T, srv *httptest.Server) ([]domain.SearchResult, error)
	Required func(t *testing.T, r domain.SearchResult)
}

func RunContracts(t *testing.T, cases []Contract) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { runContract(t, tc) })
	}
}

func runContract(t *testing.T, tc Contract) {
	golden, err := os.ReadFile(filepath.Join("testdata", tc.Golden))
	if err != nil {
		t.Fatalf("read golden %s: %v", tc.Golden, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(golden)
	}))
	defer srv.Close()

	results, err := tc.Search(t, srv)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 mapped result from the golden, got %d", len(results))
	}
	tc.Required(t, results[0])
}
