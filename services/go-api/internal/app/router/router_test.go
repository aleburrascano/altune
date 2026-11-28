package router_test

import (
	"altune/go-api/internal/app/router"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/slowclienttest"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const productionWriteTimeout = 60 * time.Second

const routeWriteDeadline = 150 * time.Millisecond

func newRouter(s router.Settings) *chi.Mux {
	if s.WriteTimeout == 0 {
		s.WriteTimeout = productionWriteTimeout
	}
	s.Latency = requestmetrics.New()
	return router.New(s)
}

func TestRouter_PanickingHandlerLogsOneErrorAndRequestCompleteAt500(t *testing.T) {
	ring := logging.CaptureRing(t, "info")

	r := newRouter(router.Settings{})
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
	if got := completes[0].Level; got != slog.LevelInfo.String() {
		t.Errorf("request.complete level = %s, want %s: the panic is the one ERROR line", got, slog.LevelInfo)
	}
	var errorLines []string
	for _, captured := range ring.Snapshot() {
		if captured.Level == slog.LevelError.String() {
			errorLines = append(errorLines, captured.Message)
		}
	}
	if len(errorLines) != 1 || errorLines[0] != "panic.recovered" {
		t.Errorf("ERROR lines = %v, want only panic.recovered", errorLines)
	}
}

func newStream(t *testing.T, d eventstream.Deps) func(chi.Router) {
	t.Helper()
	d.Now = clock.Now
	d.NewTicker = clock.NewTicker
	routes, err := eventstream.New(d)
	if err != nil {
		t.Fatalf("eventstream.New: %v", err)
	}
	return routes.LongLived
}

func newDeadlineRouter(uid ids.UserId, streamRoutes func(chi.Router), bulkErrs chan<- error) *chi.Mux {
	r := newRouter(router.Settings{WriteTimeout: routeWriteDeadline})
	r.Get("/v1/bulk", func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 16<<10)
		giveUp := time.Now().Add(10 * time.Second)
		for time.Now().Before(giveUp) {
			if _, err := w.Write(chunk); err != nil {
				bulkErrs <- err
				return
			}
		}
		bulkErrs <- nil
	})
	r.Route("/v1", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(requestuser.ContextWithUserID(req.Context(), uid)))
			})
		})
		streamRoutes(r)
	})
	return r
}

func TestRouter_WriteDeadlineCutsOffSlowReaderOnNonSSERoute(t *testing.T) {
	bulkErrs := make(chan error, 1)
	sse := newStream(t, eventstream.Deps{Bus: events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))})
	srv := slowclienttest.NewServer(t, newDeadlineRouter(ids.NewUserId(uuid.New()), sse, bulkErrs))

	began := time.Now()
	slowclienttest.Get(t, srv, "/v1/bulk", nil)

	select {
	case err := <-bulkErrs:
		if err == nil {
			t.Fatal("bulk writes never failed: slow reader held the handler open")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bulk handler still blocked in Write after 3s: no write deadline on non-SSE route")
	}
	if elapsed := time.Since(began); elapsed < routeWriteDeadline {
		t.Errorf("cut off after %s, before the %s deadline", elapsed, routeWriteDeadline)
	}
}

func TestRouter_SSEStreamOutlivesRouteWriteDeadline(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	sse := newStream(t, eventstream.Deps{Bus: bus, Heartbeat: routeWriteDeadline / 3})
	srv := slowclienttest.NewServer(t, newDeadlineRouter(uid, sse, make(chan error, 1)))

	conn, br := slowclienttest.Get(t, srv, "/v1/events", nil)
	resp := slowclienttest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := bufio.NewReader(resp.Body)

	began := time.Now()
	readUntil(t, body, func(string) bool { return time.Since(began) > 4*routeWriteDeadline })
	bus.Publish(context.Background(), uid, eventstest.Fielded("late.event", map[string]any{"k": "v"}))
	readUntil(t, body, func(l string) bool { return strings.HasPrefix(l, "event: late.event") })
}

func TestRouter_SSEPerFrameDeadlineReachesConnection(t *testing.T) {
	logging.Silence(t)

	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	sse := newStream(t, eventstream.Deps{Bus: bus, WriteTimeout: routeWriteDeadline, Heartbeat: time.Hour})
	done := make(chan struct{})
	router := newDeadlineRouter(uid, sse, make(chan error, 1))
	srv := slowclienttest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		router.ServeHTTP(w, r)
	}))

	slowclienttest.Get(t, srv, "/v1/events", nil)

	payload := map[string]any{"pad": strings.Repeat("x", 16<<10)}
	polltest.Until(t, 5*time.Second, "the SSE handler to give up on a stalled client (per-frame deadline applied)", func() bool {
		select {
		case <-done:
			return true
		default:
			bus.Publish(context.Background(), uid, eventstest.Fielded("bulk", payload))
			return false
		}
	})
}

func TestMountV1_TheRequestTimeoutBoundsTheBoundedGroupAndLeavesTheLongLivedOneAlone(t *testing.T) {
	const budget = 100 * time.Millisecond
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/v1/blocked", http.StatusGatewayTimeout},
		{http.MethodPost, "/v1/tracks/abc/audio/recover", http.StatusGatewayTimeout},
		{http.MethodGet, "/v1/events", http.StatusOK},
		{http.MethodGet, "/v1/tracks/abc/audio", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRouter(router.Settings{})
				blockUntilDone := func(w http.ResponseWriter, req *http.Request) {
					select {
					case <-req.Context().Done():
						w.WriteHeader(http.StatusGatewayTimeout)
					case <-time.After(3 * budget):
						w.WriteHeader(http.StatusOK)
					}
				}
				router.MountV1(r, budget, router.V1{
					Bounded: func(r chi.Router) {
						r.Get("/blocked", blockUntilDone)
						r.Post("/tracks/{trackId}/audio/recover", blockUntilDone)
					},
					LongLived: func(r chi.Router) {
						r.Get("/events", blockUntilDone)
						r.Get("/tracks/{trackId}/audio", blockUntilDone)
					},
				})

				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
				if rec.Code != tc.want {
					t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
				}
			})
		})
	}
}

func TestMountV1_EveryGroupCarriesTheV1ResponseHeaders(t *testing.T) {
	r := newRouter(router.Settings{})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.MountV1(r, time.Minute, router.V1{
		Bounded:   func(r chi.Router) { r.Get("/bounded", ok) },
		LongLived: func(r chi.Router) { r.Get("/long-lived", ok) },
	})

	for _, path := range []string{"/v1/bounded", "/v1/long-lived"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestRouter_CORSPreflightAllowsIdempotencyKeyOnlyForAllowedOrigin(t *testing.T) {
	const allowed = "http://127.0.0.1:8081"
	r := newRouter(router.Settings{CORSOrigins: []string{allowed}})
	r.Post("/v1/tracks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	preflight := func(origin, headers string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/v1/tracks", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", headers)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := preflight(allowed, "authorization,content-type,idempotency-key")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowed {
		t.Errorf("Allow-Origin = %q, want %q", got, allowed)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "idempotency-key") {
		t.Errorf("Allow-Headers = %q, want it to contain Idempotency-Key", got)
	}

	rec = preflight("http://evil.example", "authorization,content-type,idempotency-key")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("look-alike origin got Allow-Origin %q, want none", got)
	}

	rec = preflight(allowed, "authorization,x-not-allowed")
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "" {
		t.Errorf("disallowed header got Allow-Headers %q, want none", got)
	}
}

func TestRouter_CORSPreflightAllowsTheCorrelationAndResumeHeadersWithoutCredentials(t *testing.T) {
	const allowed = "http://127.0.0.1:8081"
	r := newRouter(router.Settings{CORSOrigins: []string{allowed}})
	r.Get("/v1/events", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodOptions, "/v1/events", nil)
	req.Header.Set("Origin", allowed)
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "authorization,x-correlation-id,last-event-id")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	allowedHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	for _, want := range []string{"x-correlation-id", "last-event-id"} {
		if !strings.Contains(allowedHeaders, want) {
			t.Errorf("Allow-Headers = %q, want it to contain %s", allowedHeaders, want)
		}
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want none: the API uses no cookies", got)
	}
}

func TestRouter_CORSExposesTheCorrelationIDAndRetryAfterToTheAllowedOrigin(t *testing.T) {
	const allowed = "http://127.0.0.1:8081"
	r := newRouter(router.Settings{CORSOrigins: []string{allowed}})
	r.Get("/v1/tracks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks", nil)
	req.Header.Set("Origin", allowed)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	exposed := strings.ToLower(rec.Header().Get("Access-Control-Expose-Headers"))
	for _, want := range []string{"x-correlation-id", "retry-after", "content-length"} {
		if !strings.Contains(exposed, want) {
			t.Errorf("Expose-Headers = %q, want it to contain %s", exposed, want)
		}
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want none: the API uses no cookies", got)
	}
	if rec.Header().Get("X-Correlation-ID") == "" {
		t.Error("response carries no X-Correlation-ID for the client to read")
	}
}

func TestRouter_CORSPreflightRefusesTheRetiredAnalyticsHeader(t *testing.T) {
	const allowed = "http://127.0.0.1:8081"
	r := newRouter(router.Settings{CORSOrigins: []string{allowed}})
	r.Post("/v1/discovery/events", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodOptions, "/v1/discovery/events", nil)
	req.Header.Set("Origin", allowed)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,x-altune-analytics")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "" {
		t.Errorf("the retired analytics opt-out header got Allow-Headers %q, want none", got)
	}
}

func readUntil(t *testing.T, r *bufio.Reader, match func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading stream: %v", err)
		}
		if match(strings.TrimRight(line, "\n")) {
			return line
		}
	}
	t.Fatal("timed out waiting for expected line")
	return ""
}

func TestV1ResponseHeaders_HandlerCacheControlOverridesDefault(t *testing.T) {
	h := router.V1ResponseHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/discovery/search", nil))
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want handler value private, no-store", got)
	}
}

func TestRouter_HealthAndLongLivedRoutesAreNotTraced(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	r := router.New(router.Settings{WriteTimeout: time.Minute, Latency: requestmetrics.New()})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	r.Get("/health", ok)
	router.MountV1(r, time.Minute, router.V1{
		Bounded: func(r chi.Router) {
			r.Post("/tracks/{id}/audio", ok)
			r.Get("/library", ok)
		},
		LongLived: func(r chi.Router) {
			r.Get("/events", ok)
			r.Get("/tracks/{id}/audio", ok)
		},
	})

	for _, req := range []struct {
		method, path string
		traced       bool
	}{
		{http.MethodGet, "/health", false},
		{http.MethodGet, "/v1/events", false},
		{http.MethodGet, "/v1/tracks/1/audio", false},
		{http.MethodPost, "/v1/tracks/1/audio", true},
		{http.MethodGet, "/v1/library", true},
	} {
		before := len(recorder.Ended())
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(req.method, req.path, nil))
		if traced := len(recorder.Ended()) > before; traced != req.traced {
			t.Errorf("%s %s traced = %v, want %v", req.method, req.path, traced, req.traced)
		}
	}
}

func TestRouter_TheRequestCompleteLineCarriesTheTraceIdOfTracedRoutesOnly(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	logs := logging.CaptureJSON(t, slog.LevelInfo)
	r := router.New(router.Settings{WriteTimeout: time.Minute, Latency: requestmetrics.New()})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.MountV1(r, time.Minute, router.V1{
		Bounded:   func(r chi.Router) { r.Get("/library", ok) },
		LongLived: func(r chi.Router) { r.Get("/events", ok) },
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/library", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/events", nil))

	traceIDs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec["msg"] != "request.complete" {
			continue
		}
		id, _ := rec["trace_id"].(string)
		traceIDs[rec["path"].(string)] = id
	}
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("spans = %d, want one for the bounded route", len(ended))
	}
	if want := ended[0].SpanContext().TraceID().String(); traceIDs["/v1/library"] != want {
		t.Errorf("trace_id on the bounded route's line = %q, want the span's %q", traceIDs["/v1/library"], want)
	}
	if got, seen := traceIDs["/v1/events"]; !seen || got != "" {
		t.Errorf("long-lived route line: seen = %v, trace_id = %q, want a line with no trace_id", seen, got)
	}
}

func TestRouter_RecordsEachRequestsLatencyByRouteAndStatusClass(t *testing.T) {
	latency := requestmetrics.New()
	r := router.New(router.Settings{WriteTimeout: time.Minute, Latency: latency})
	const route = "/v1/latency-probe/{status}"
	r.Get(route, func(w http.ResponseWriter, req *http.Request) {
		status, _ := strconv.Atoi(chi.URLParam(req, "status"))
		w.WriteHeader(status)
	})

	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusBadGateway} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/latency-probe/"+strconv.Itoa(status), nil))
	}

	rl := latency.Snapshot().Routes[route]
	if rl.Count != 3 || rl.Status.Count2xx != 1 || rl.Status.Count4xx != 1 || rl.Status.Count5xx != 1 {
		t.Errorf("latency.routes[%q] = %+v, want count 3 and one each of 2xx/4xx/5xx", route, rl)
	}
}
