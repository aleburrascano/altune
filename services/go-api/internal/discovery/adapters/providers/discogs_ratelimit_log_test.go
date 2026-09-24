package providers

import (
	"altune/go-api/internal/discovery/domain"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscogsAdapter_429LogOmitsQuery(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()
	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	_, _ = adapter.Resolve(context.Background(), domain.ResultKindArtist, "SecretQueryText", "", "")

	out := buf.String()
	if !strings.Contains(out, "discogs.rate_limited") {
		t.Fatalf("expected rate-limit log, got %q", out)
	}
	if strings.Contains(out, "SecretQueryText") || strings.Contains(out, "?") {
		t.Errorf("log leaks query: %q", out)
	}
}
