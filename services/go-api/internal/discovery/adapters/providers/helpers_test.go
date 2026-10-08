package providers

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const hostileMBID = "a/b?c#d"

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

const sensitiveQuery = "zqxj private diagnosis clinic"

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
	return providertest.ArtworkFailureHandlers()
}

func artworkProviderVerifiedMissHandlers() map[string]http.HandlerFunc {
	return providertest.ArtworkVerifiedMissHandlers()
}

func cancelMidRequestServer(t *testing.T) (*httptest.Server, context.Context) {
	t.Helper()
	return providertest.CancelMidRequestServer(t)
}
