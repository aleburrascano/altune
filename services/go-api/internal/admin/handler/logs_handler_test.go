package handler

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFilterByLevel(t *testing.T) {
	records := []logging.CapturedRecord{
		{Level: "DEBUG", Message: "d"},
		{Level: "INFO", Message: "i"},
		{Level: "WARN", Message: "w"},
		{Level: "ERROR", Message: "e"},
	}

	tests := []struct {
		name    string
		min     string
		wantLen int
	}{
		{"warn and above", "WARN", 2},
		{"error only", "ERROR", 1},
		{"info and above", "INFO", 3},
		{"debug keeps all", "DEBUG", 4},
		{"warn with offset suffix still ranks as warn", "WARN+2", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(filterByLevel(records, tt.min)); got != tt.wantLen {
				t.Errorf("filterByLevel(%q) len = %d, want %d", tt.min, got, tt.wantLen)
			}
		})
	}
}

// TestServeLogs_RedactsAPIKeyFromWrappedURLError is the end-to-end guard for
// #997: a provider failure logged with a *url.Error carrying the Last.fm
// api_key must not be served by GET /admin/logs.
func TestServeLogs_RedactsAPIKeyFromWrappedURLError(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	const apiKey = "livelastfmkey0123456789"
	err := &url.Error{
		Op:  "Get",
		URL: "https://ws.audioscrobbler.com/2.0/?method=artist.search&api_key=" + apiKey + "&format=json",
		Err: context.DeadlineExceeded,
	}
	slog.WarnContext(context.Background(), "provider search failed", "provider", "lastfm", "error", err)

	h := New(nil, ring)
	rec := httptest.NewRecorder()
	h.serveLogs(rec, httptest.NewRequest(http.MethodGet, "/admin/logs", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "provider search failed") {
		t.Fatalf("log record not served: %s", body)
	}
	if strings.Contains(body, apiKey) {
		t.Fatalf("api_key served by /admin/logs: %s", body)
	}
}
