package app

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_PanickingHandlerLogsRequestCompleteAs500Error(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	a := &App{cfg: &config.Config{Env: "test"}}
	r := a.newRouter(apiWriteTimeout)
	r.Get("/v1/boom", func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want 500", rec.Code)
	}
	var completes []logging.CapturedRecord
	for _, captured := range ring.Snapshot() {
		if captured.Message == "request.complete" {
			completes = append(completes, captured)
		}
	}
	if len(completes) != 1 {
		t.Fatalf("request.complete records = %d, want 1", len(completes))
	}
	if got := completes[0].Attrs["status"]; got != "500" {
		t.Errorf("request.complete status = %s, want 500", got)
	}
	if got := completes[0].Level; got != slog.LevelError.String() {
		t.Errorf("request.complete level = %s, want %s", got, slog.LevelError)
	}
}
