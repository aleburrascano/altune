package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

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

func TestSearchAcrossKinds_DoesNotLogQueryText(t *testing.T) {
	buf := captureDefaultLog(t)
	all := allSearchKinds()

	// The provider error embeds the request URL, as *url.Error does.
	_, _ = searchAcrossKinds(context.Background(), "deezer", sensitiveQuery, all, all,
		func(_ context.Context, _ domain.ResultKind) ([]domain.SearchResult, error) {
			return nil, fmt.Errorf("get \"https://api.example/search?q=%s\": timeout", url.QueryEscape(sensitiveQuery))
		})

	logged := buf.String()
	if !strings.Contains(logged, "deezer.search_kind_failed") || !strings.Contains(logged, "timeout") {
		t.Fatalf("expected the kind failure (with its error cause) to still be logged, got:\n%s", logged)
	}
	assertNoQueryText(t, logged)
}

type failingFallback struct{ called bool }

func (f *failingFallback) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	f.called = true
	return nil, errors.New("fallback down")
}

func TestSoundCloudSearch_FallbackDoesNotLogQueryText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	buf := captureDefaultLog(t)

	fb := &failingFallback{}
	a := newTestSoundCloudAPI(srv, fb)
	_, _ = a.Search(context.Background(), sensitiveQuery, trackKinds())

	logged := buf.String()
	if !fb.called || !strings.Contains(logged, "soundcloud.apiv2_fallback") {
		t.Fatalf("expected the api-v2 fallback to be taken and logged, got:\n%s", logged)
	}
	assertNoQueryText(t, logged)
}
