package handler_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type stubQualityReader struct {
	cases      []ports.DiscographyCase
	casesErr   error
	suspectErr error
}

func (s stubQualityReader) DiscographyQuality(context.Context, time.Time, ports.DiscographyGroupBy, int) ([]ports.DiscographyCase, error) {
	return s.cases, s.casesErr
}

func (s stubQualityReader) SuspectRate(context.Context, time.Time) (ports.DiscographySuspectRate, error) {
	return ports.DiscographySuspectRate{Rate: 0.5}, s.suspectErr
}

func query(r stubQualityReader) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	handler.NewObserveHandler(clock.Now, clock.NewTicker, handler.Deps{Discography: r}).Register(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/quality/discography", nil))
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return body
}

func TestDiscographyQuality_DeadlineExceededMapsToTimeout(t *testing.T) {
	for name, r := range map[string]stubQualityReader{
		"cases":   {casesErr: context.DeadlineExceeded},
		"suspect": {suspectErr: context.DeadlineExceeded},
	} {
		rec := query(r)
		if code := string(decodeBody(t, rec)["code"]); rec.Code != http.StatusGatewayTimeout || code != `"observe.discography_quality_timeout"` {
			t.Errorf("%s: status %d body %s, want 504 observe.discography_quality_timeout", name, rec.Code, rec.Body.String())
		}
	}
}

func TestDiscographyQuality_ATimeoutLogsItsCauseButAnswersOnlyTheTimeout(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelInfo)

	rec := query(stubQualityReader{casesErr: context.DeadlineExceeded})

	line := logs.String()
	for _, want := range []string{`"code":"observe.discography_quality_timeout"`, "context deadline exceeded"} {
		if !strings.Contains(line, want) {
			t.Errorf("failure log %s lacks %s", line, want)
		}
	}
	if detail := string(decodeBody(t, rec)["detail"]); detail != `"discography quality query timed out"` {
		t.Errorf("detail %s, want only the timeout, never its cause", detail)
	}
}

func TestDiscographyQuality_OtherErrorsPassThrough(t *testing.T) {
	rec := query(stubQualityReader{casesErr: context.Canceled})

	if code := string(decodeBody(t, rec)["code"]); rec.Code != http.StatusInternalServerError || code != `"internal"` {
		t.Fatalf("status %d body %s, want the unmapped error answered as 500 internal", rec.Code, rec.Body.String())
	}
}

func TestDiscographyQuality_ZeroCasesGivesAnEmptyList(t *testing.T) {
	rec := query(stubQualityReader{})

	if cases := string(decodeBody(t, rec)["cases"]); rec.Code != http.StatusOK || cases != "[]" {
		t.Fatalf("status %d cases %s, want 200 with an empty list, never null", rec.Code, cases)
	}
}
