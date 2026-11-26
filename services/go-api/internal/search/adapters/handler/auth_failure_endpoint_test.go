package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const validAuthFailureBody = `{"reason":"invalid_credentials","app_version":"1.2.3"}`

func buildAuthFailureRouter(store *recordingEventStore, limit search.RequestLimit) chi.Router {
	h := search.MustNewDiscoveryHandler(search.DiscoveryServices{Event: search.NewRecordEventService(store)}, time.Now)
	r := chi.NewRouter()
	h.PublicRoutes(r, limit)
	return r
}

func postAuthFailure(r chi.Router, body, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/public/auth-failures", strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAuthFailureEndpoint_RecordsAnonymousEvent(t *testing.T) {
	store := &recordingEventStore{}
	r := buildAuthFailureRouter(store, search.DefaultAuthFailureLimit)

	rec := postAuthFailure(r, validAuthFailureBody, "203.0.113.5:1000")

	discAssertStatus(t, rec, http.StatusNoContent)
	if len(store.events) != 1 {
		t.Fatalf("appended %d events, want 1", len(store.events))
	}
	got := store.events[0]
	if got.Type != search.EventTypeAuthFailed || got.UserId != ids.AnonymousUserId() {
		t.Errorf("event = %v for %v, want auth_failed for the anonymous user", got.Type, got.UserId)
	}
	want := map[string]any{"reason": "invalid_credentials", "app_version": "1.2.3"}
	if !reflect.DeepEqual(got.Payload, want) {
		t.Errorf("payload = %v, want %v", got.Payload, want)
	}
}

func TestAuthFailureEndpoint_RateLimitsPerClientIP(t *testing.T) {
	store := &recordingEventStore{}
	r := buildAuthFailureRouter(store, search.RequestLimit{Max: 2, Window: time.Minute})

	for range 2 {
		discAssertStatus(t, postAuthFailure(r, validAuthFailureBody, "203.0.113.5:1000"), http.StatusNoContent)
	}
	rec := postAuthFailure(r, validAuthFailureBody, "203.0.113.5:1001")
	discAssertStatus(t, rec, http.StatusTooManyRequests)
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 carries no Retry-After header")
	}
	if len(store.events) != 2 {
		t.Errorf("appended %d events, want 2 (the rejected call records nothing)", len(store.events))
	}

	discAssertStatus(t, postAuthFailure(r, validAuthFailureBody, "203.0.113.6:1000"), http.StatusNoContent)
}

func TestAuthFailureEndpoint_RejectsInvalidBodies(t *testing.T) {
	cases := map[string]string{
		"unknown field":        `{"reason":"network","app_version":"1.2.3","email":"a@b.c"}`,
		"missing reason":       `{"app_version":"1.2.3"}`,
		"missing app_version":  `{"reason":"network"}`,
		"reason outside enum":  `{"reason":"bad_password","app_version":"1.2.3"}`,
		"version too long":     `{"reason":"network","app_version":"` + strings.Repeat("1", 33) + `"}`,
		"version with space":   `{"reason":"network","app_version":"1 2"}`,
		"trailing data":        validAuthFailureBody + `{}`,
		"over 512 bytes":       `{"reason":"network","app_version":"1.2.3","pad":"` + strings.Repeat("x", 600) + `"}`,
		"oversized valid keys": validAuthFailureBody + strings.Repeat(" ", 600),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			store := &recordingEventStore{}
			r := buildAuthFailureRouter(store, search.DefaultAuthFailureLimit)

			rec := postAuthFailure(r, body, "203.0.113.5:1000")

			discAssertStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), "auth_failure.invalid_body") {
				t.Errorf("body = %s, want code %s", rec.Body.String(), "auth_failure.invalid_body")
			}
			if len(store.events) != 0 {
				t.Errorf("appended %d events, want 0", len(store.events))
			}
		})
	}
}
