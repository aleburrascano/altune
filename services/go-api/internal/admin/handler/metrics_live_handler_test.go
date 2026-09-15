package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/reqmetrics"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
)

// injectUser mirrors auth.Middleware for tests: it puts a user id in the request
// context when present, otherwise leaves the request unauthenticated so
// OperatorOnly's auth.RequireUserID rejects it.
func injectUser(id shared.UserId, present bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if present {
				r = r.WithContext(auth.ContextWithUserID(r.Context(), id))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// mountAdmin builds the same /admin group shape as internal/app/admin_wiring.go:
// the data routes sit behind an auth middleware and OperatorOnly.
func mountAdmin(operatorID string, caller shared.UserId, authed bool) http.Handler {
	h := handler.New(nil, nil)
	r := chi.NewRouter()
	r.Route("/admin", func(ar chi.Router) {
		ar.Group(func(gr chi.Router) {
			gr.Use(injectUser(caller, authed))
			gr.Use(handler.OperatorOnly(operatorID))
			h.RegisterData(gr)
		})
	})
	return r
}

func TestMetricsLive_OperatorOnly(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())

	tests := []struct {
		name       string
		caller     shared.UserId
		authed     bool
		wantStatus int
	}{
		{"unauthenticated is rejected", shared.UserId{}, false, http.StatusUnauthorized},
		{"non-operator is forbidden", other, true, http.StatusForbidden},
		{"operator is allowed", operator, true, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mountAdmin(operator.String(), tt.caller, tt.authed)
			req := httptest.NewRequest(http.MethodGet, "/admin/metrics/live", nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestMetricsLive_OperatorGetsCounters(t *testing.T) {
	operator := shared.NewUserId(uuid.New())

	// Move the live counters via the real adapters; the endpoint must reflect them.
	before := struct {
		Auth     authmetrics.Snapshot
		Catalog  catalogmetrics.Snapshot
		Feedback feedbackmetrics.Snapshot
	}{authmetrics.ReadSnapshot(), catalogmetrics.ReadSnapshot(), feedbackmetrics.ReadSnapshot()}
	authmetrics.NewExpvarAuthMetrics().TokenRejected("signature_invalid")
	authmetrics.NewExpvarAuthMetrics().VerifierUnavailable()
	authmetrics.NewExpvarAuthMetrics().JWKSFetchFailed()
	catalogmetrics.NewExpvarAudioStoreMetrics().PresignFailed()
	feedbackmetrics.NewExpvarFeedbackMetrics().TrackerCreateFailed()

	srv := mountAdmin(operator.String(), operator, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/metrics/live", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var got struct {
		Auth     authmetrics.Snapshot     `json:"auth"`
		Catalog  catalogmetrics.Snapshot  `json:"catalog"`
		Feedback feedbackmetrics.Snapshot `json:"feedback"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}
	if got.Auth.TokenRejections != before.Auth.TokenRejections+1 {
		t.Errorf("auth token_rejections_total = %d, want %d",
			got.Auth.TokenRejections, before.Auth.TokenRejections+1)
	}
	if want := before.Auth.TokenRejectionsByReason["signature_invalid"] + 1; got.Auth.TokenRejectionsByReason["signature_invalid"] != want {
		t.Errorf("auth token_rejections_by_reason_total[signature_invalid] = %d, want %d",
			got.Auth.TokenRejectionsByReason["signature_invalid"], want)
	}
	if got.Auth.VerifierUnavailable != before.Auth.VerifierUnavailable+1 {
		t.Errorf("auth verifier_unavailable_total = %d, want %d",
			got.Auth.VerifierUnavailable, before.Auth.VerifierUnavailable+1)
	}
	if got.Auth.JWKSFetchFailures != before.Auth.JWKSFetchFailures+1 {
		t.Errorf("auth jwks_fetch_failures_total = %d, want %d",
			got.Auth.JWKSFetchFailures, before.Auth.JWKSFetchFailures+1)
	}
	if got.Catalog.PresignFailures != before.Catalog.PresignFailures+1 {
		t.Errorf("catalog presign_failures_total = %d, want %d",
			got.Catalog.PresignFailures, before.Catalog.PresignFailures+1)
	}
	if got.Feedback.TrackerCreateFailures != before.Feedback.TrackerCreateFailures+1 {
		t.Errorf("feedback tracker_create_failures_total = %d, want %d",
			got.Feedback.TrackerCreateFailures, before.Feedback.TrackerCreateFailures+1)
	}
}

// TestMetricsLive_IncludesRouteLatency proves the extended endpoint exposes the
// per-route latency histogram alongside the counters, behind the operator gate.
func TestMetricsLive_IncludesRouteLatency(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	const route = "/v1/handler-endpoint-probe/{id}"
	reqmetrics.Observe(route, 4*time.Millisecond)

	srv := mountAdmin(operator.String(), operator, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/metrics/live", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Latency reqmetrics.Snapshot `json:"latency"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}
	rl, ok := got.Latency.Routes[route]
	if !ok {
		t.Fatalf("latency.routes missing %q; body %q", route, rec.Body.String())
	}
	if rl.Count == 0 {
		t.Errorf("latency.routes[%q].count = 0, want >= 1", route)
	}
}

// TestMetricsLive_DoesNotLeakProcessGlobals proves the response carries only the
// named module counters — never the raw expvar dump's process globals.
func TestMetricsLive_DoesNotLeakProcessGlobals(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	srv := mountAdmin(operator.String(), operator, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/metrics/live", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, leak := range []string{"cmdline", "memstats"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaks process global %q: %s", leak, body)
		}
	}
}

// TestRawExpvarNotMounted asserts the raw /debug/vars handler is never served by
// our router. expvar's init registers /debug/vars on http.DefaultServeMux at
// import time, so the counters would leak world-readable if the app used the
// default mux; the app uses a chi router, and this proves it does not carry the
// raw handler.
func TestRawExpvarNotMounted(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	srv := mountAdmin(operator.String(), operator, true)

	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/debug/vars status = %d, want 404 (raw expvar must not be mounted)", rec.Code)
	}

	// Sanity: the leak vector is real — the default mux does serve it — so the
	// 404 above is a deliberate omission, not an accident of expvar being absent.
	drec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(drec, httptest.NewRequest(http.MethodGet, "/debug/vars", nil))
	if drec.Code != http.StatusOK {
		t.Fatalf("expected expvar to register /debug/vars on the default mux, got %d", drec.Code)
	}
}
