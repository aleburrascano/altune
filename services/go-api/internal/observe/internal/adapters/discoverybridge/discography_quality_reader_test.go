package discoverybridge_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type stubQualityReader struct {
	cases      []discovery.DiscographyCase
	casesErr   error
	suspectErr error
}

func (s stubQualityReader) DiscographyQuality(context.Context, time.Time, discovery.DiscographyGroupBy, int) ([]discovery.DiscographyCase, error) {
	return s.cases, s.casesErr
}

func (s stubQualityReader) SuspectRate(context.Context, time.Time) (discovery.DiscographySuspectRate, error) {
	return discovery.DiscographySuspectRate{Rate: 0.5}, s.suspectErr
}

func query(r stubQualityReader) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	observe.NewObserveHandler(observe.Deps{Discography: r}).Register(router)
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

func TestQuery_DeadlineExceededMapsToTimeout(t *testing.T) {
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

func TestQuery_OtherErrorsPassThrough(t *testing.T) {
	rec := query(stubQualityReader{casesErr: context.Canceled})

	if code := string(decodeBody(t, rec)["code"]); rec.Code != http.StatusInternalServerError || code != `"internal"` {
		t.Fatalf("status %d body %s, want the unmapped error answered as 500 internal", rec.Code, rec.Body.String())
	}
}

func TestQuery_ZeroCasesGivesAnEmptyList(t *testing.T) {
	rec := query(stubQualityReader{})

	if cases := string(decodeBody(t, rec)["cases"]); rec.Code != http.StatusOK || cases != "[]" {
		t.Fatalf("status %d cases %s, want 200 with an empty list, never null", rec.Code, cases)
	}
}
