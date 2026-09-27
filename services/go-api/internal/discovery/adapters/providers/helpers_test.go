package providers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// hostileMBID carries every URL-special character that could restructure an
// outbound request if interpolated raw: a path separator, a query start and a
// fragment start. MBIDs come verbatim from third-party JSON (issue #572).
const hostileMBID = "a/b?c#d"

// capturingClient records the outbound request and answers with an empty 404,
// so nothing leaves the process.
func capturingClient(capture **http.Request) *http.Client {
	return &http.Client{Transport: fakeRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		*capture = r
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	}}}
}

// sensitiveQuery is distinctive enough that any substring hit in captured log
// output can only come from the user's search text.
const sensitiveQuery = "zqxj private diagnosis clinic"

// captureDefaultLog routes the default slog logger into a JSON buffer, as the
// production stdout handler does, and restores it when the test ends.
func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func assertNoQueryText(t *testing.T, logged string) {
	t.Helper()
	for _, form := range []string{sensitiveQuery, url.QueryEscape(sensitiveQuery), url.PathEscape(sensitiveQuery), "diagnosis"} {
		if strings.Contains(logged, form) {
			t.Fatalf("log output contains search text %q:\n%s", form, logged)
		}
	}
}

func artworkProviderFailureHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"429 with Retry-After": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		"503": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		},
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
		"redirect loop": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
		},
		"302 without a Location": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusFound)
		},
	}
}

func artworkProviderVerifiedMissHandlers() map[string]http.HandlerFunc {
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

func cancelMidRequestServer(t *testing.T) (*httptest.Server, context.Context) {
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
