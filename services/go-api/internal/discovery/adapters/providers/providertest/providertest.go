package providertest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.targetURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

func NewClient(serverURL string) *http.Client {
	return &http.Client{Transport: &redirectTransport{targetURL: serverURL}}
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
