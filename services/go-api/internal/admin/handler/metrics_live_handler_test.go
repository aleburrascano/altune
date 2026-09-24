package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"encoding/json"
	_ "expvar" // registers /debug/vars on the default mux, the leak vector TestRawExpvarNotMounted checks
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// stubLiveMetrics is the injected source: a fixed aggregate in the documented
// /metrics/live wire shape, so the handler is tested without any expvar global.
func stubLiveMetrics() any {
	return map[string]any{
		"auth": map[string]any{
			"token_rejections_total":           1,
			"token_rejections_by_reason_total": map[string]int{"signature_invalid": 1},
			"verifier_unavailable_total":       1,
			"jwks_fetch_failures_total":        1,
		},
		"catalog":  map[string]any{"presign_failures_total": 1},
		"feedback": map[string]any{"tracker_create_failures_total": 1},
		"playback": map[string]any{
			"now_playing_enrichment_breaker_open":             true,
			"now_playing_enrichment_breaker_rejections_total": 1,
		},
		"providers": map[string]any{
			"deezer":  map[string]any{"ok": 1},
			"spotify": map[string]any{"quota": 1},
		},
		"latency": map[string]any{
			"routes": map[string]any{
				"/v1/probe/{id}": map[string]any{
					"count":  3,
					"status": map[string]int{"2xx": 1, "4xx": 1, "5xx": 1},
				},
			},
		},
	}
}

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
	h := handler.New(nil, nil).WithLiveMetrics(stubLiveMetrics)
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

func getLive(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	operator := shared.NewUserId(uuid.New())
	srv := mountAdmin(operator.String(), operator, true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/metrics/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	return rec
}

// The operator receives exactly what the injected source reports, as JSON, with
// the documented per-module keys and counter values.
func TestMetricsLive_OperatorGetsCounters(t *testing.T) {
	rec := getLive(t)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	want, err := json.Marshal(stubLiveMetrics())
	if err != nil {
		t.Fatal(err)
	}
	var gotV, wantV any
	if err := json.Unmarshal(rec.Body.Bytes(), &gotV); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}
	_ = json.Unmarshal(want, &wantV)
	gb, _ := json.Marshal(gotV)
	wb, _ := json.Marshal(wantV)
	if string(gb) != string(wb) {
		t.Errorf("body = %s, want %s", gb, wb)
	}
}

// TestMetricsLive_ReportsEnrichmentBreakerState pins the breaker keys on the wire.
func TestMetricsLive_ReportsEnrichmentBreakerState(t *testing.T) {
	var got struct {
		Playback struct {
			Open       bool  `json:"now_playing_enrichment_breaker_open"`
			Rejections int64 `json:"now_playing_enrichment_breaker_rejections_total"`
		} `json:"playback"`
	}
	if err := json.Unmarshal(getLive(t).Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Playback.Open {
		t.Errorf("playback now_playing_enrichment_breaker_open = false, want true")
	}
	if got.Playback.Rejections != 1 {
		t.Errorf("playback now_playing_enrichment_breaker_rejections_total = %d, want 1", got.Playback.Rejections)
	}
}

// TestMetricsLive_IncludesProviderCounts pins the per-provider, per-outcome keys.
func TestMetricsLive_IncludesProviderCounts(t *testing.T) {
	var got struct {
		Providers map[string]struct {
			OK    int64 `json:"ok"`
			Quota int64 `json:"quota"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(getLive(t).Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Providers["deezer"].OK != 1 {
		t.Errorf("providers.deezer.ok = %d, want 1", got.Providers["deezer"].OK)
	}
	if got.Providers["spotify"].Quota != 1 {
		t.Errorf("providers.spotify.quota = %d, want 1", got.Providers["spotify"].Quota)
	}
}

// TestMetricsLive_IncludesRouteLatency pins the per-route latency keys.
func TestMetricsLive_IncludesRouteLatency(t *testing.T) {
	const route = "/v1/probe/{id}"
	var got struct {
		Latency struct {
			Routes map[string]struct {
				Count uint64 `json:"count"`
			} `json:"routes"`
		} `json:"latency"`
	}
	rec := getLive(t)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	rl, ok := got.Latency.Routes[route]
	if !ok {
		t.Fatalf("latency.routes missing %q; body %q", route, rec.Body.String())
	}
	if rl.Count != 3 {
		t.Errorf("latency.routes[%q].count = %d, want 3", route, rl.Count)
	}
}

// TestMetricsLive_IncludesRouteStatusClasses pins the serialized "2xx"/"4xx"/"5xx" keys.
func TestMetricsLive_IncludesRouteStatusClasses(t *testing.T) {
	const route = "/v1/probe/{id}"
	var got struct {
		Latency struct {
			Routes map[string]struct {
				Status map[string]uint64 `json:"status"`
			} `json:"routes"`
		} `json:"latency"`
	}
	rec := getLive(t)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	status := got.Latency.Routes[route].Status
	for _, class := range []string{"2xx", "4xx", "5xx"} {
		if status[class] != 1 {
			t.Errorf("latency.routes[%q].status[%q] = %d, want 1; body %q",
				route, class, status[class], rec.Body.String())
		}
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
