package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/timingtest"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const productionWriteTimeout = 60 * time.Second

const routeWriteDeadline = 150 * time.Millisecond

func newRouter(s app.RouterSettings) *chi.Mux {
	if s.WriteTimeout == 0 {
		s.WriteTimeout = productionWriteTimeout
	}
	s.Latency = requestmetrics.New()
	return app.BuildRouter(s)
}

func TestRouter_PanickingHandlerLogsOneErrorAndRequestCompleteAt500(t *testing.T) {
	ring := logging.CaptureRing(t, "info")

	r := newRouter(app.RouterSettings{})
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
	r := newRouter(app.RouterSettings{WriteTimeout: routeWriteDeadline})
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
	srv := timingtest.NewSmallBufferServer(t, newDeadlineRouter(ids.NewUserId(uuid.New()), sse, bulkErrs))

	began := time.Now()
	timingtest.SendGet(t, srv, "/v1/bulk", nil)

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
	srv := timingtest.NewSmallBufferServer(t, newDeadlineRouter(uid, sse, make(chan error, 1)))

	conn, br := timingtest.SendGet(t, srv, "/v1/events", nil)
	resp := timingtest.ReadResponse(t, conn, br)
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
	srv := timingtest.NewSmallBufferServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		router.ServeHTTP(w, r)
	}))

	timingtest.SendGet(t, srv, "/v1/events", nil)

	payload := map[string]any{"pad": strings.Repeat("x", 16<<10)}
	timingtest.Until(t, 5*time.Second, "the SSE handler to give up on a stalled client (per-frame deadline applied)", func() bool {
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
				r := newRouter(app.RouterSettings{})
				blockUntilDone := func(w http.ResponseWriter, req *http.Request) {
					select {
					case <-req.Context().Done():
						w.WriteHeader(http.StatusGatewayTimeout)
					case <-time.After(3 * budget):
						w.WriteHeader(http.StatusOK)
					}
				}
				app.MountV1(r, budget, app.V1Routes{
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
	r := newRouter(app.RouterSettings{})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	app.MountV1(r, time.Minute, app.V1Routes{
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
	r := newRouter(app.RouterSettings{CORSOrigins: []string{allowed}})
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
	r := newRouter(app.RouterSettings{CORSOrigins: []string{allowed}})
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
	r := newRouter(app.RouterSettings{CORSOrigins: []string{allowed}})
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
	r := newRouter(app.RouterSettings{CORSOrigins: []string{allowed}})
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
	h := app.V1ResponseHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	r := app.BuildRouter(app.RouterSettings{WriteTimeout: time.Minute, Latency: requestmetrics.New()})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	r.Get("/health", ok)
	app.MountV1(r, time.Minute, app.V1Routes{
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
	r := app.BuildRouter(app.RouterSettings{WriteTimeout: time.Minute, Latency: requestmetrics.New()})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	app.MountV1(r, time.Minute, app.V1Routes{
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
	r := app.BuildRouter(app.RouterSettings{WriteTimeout: time.Minute, Latency: latency})
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

func TestNew_AnInFlightRequestFinishesWithALiveContextWhileTheServerDrains(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		result <- r.Context().Err()
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := app.NewHTTPServer(listener.Addr().String(), slow)
	go func() { _ = srv.Serve(listener) }()
	go func() {
		if resp, err := http.Get("http://" + listener.Addr().String()); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started

	drained := make(chan error, 1)
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDrain()
	go func() { drained <- srv.Shutdown(drainCtx) }()
	timingtest.Until(t, 2*time.Second, "Shutdown to stop accepting connections", func() bool {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	})
	close(release)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("in-flight request context cancelled while the server drained: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
	if err := <-drained; err != nil {
		t.Errorf("Shutdown = %v, want the drain to finish once the request did", err)
	}
}

func TestNew_ServesTheHandlerAtTheAddressWithTheFixedTimeouts(t *testing.T) {
	h := http.NotFoundHandler()

	srv := app.NewHTTPServer("127.0.0.1:8000", h)

	if srv.Addr != "127.0.0.1:8000" || srv.Handler == nil {
		t.Errorf("server = %q %v, want the given address and handler", srv.Addr, srv.Handler)
	}
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second || srv.IdleTimeout != 120*time.Second {
		t.Errorf("timeouts = header %v, read %v, idle %v, want 10s, 30s, 120s", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
}

func productionRouter(t *testing.T) *chi.Mux {
	t.Helper()
	return productionRouterFor(t, jwksConfig(t))
}

func productionRouterFor(t *testing.T, cfg *config.Config) *chi.Mux {
	t.Helper()
	return assembledRouter(t, cfg, unreachablePool(t))
}

func assembledRouter(t *testing.T, cfg *config.Config, pool *pgxpool.Pool) *chi.Mux {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: pool, Transport: refuseEgress{}, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown(context.Background())
	})
	mux, ok := assembly.Handler().(*chi.Mux)
	if !ok {
		t.Fatalf("Handler() is a %T, want the production *chi.Mux route table", assembly.Handler())
	}
	return mux
}

func TestRouter_MountsNoAdminRoute(t *testing.T) {
	r := productionRouter(t)

	var observed int
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/admin") {
			t.Errorf("%s %s: the /admin tree is gone and no route may come back under it", method, route)
		}
		if strings.HasPrefix(route, "/observe/") {
			observed++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	if observed == 0 {
		t.Fatal("walked no /observe route, so the router under test is not the production one")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/health", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin/health = %d, want 404", rec.Code)
	}
}

func TestRouter_MountsNoTestLoginInDevelopment(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.Env = "development"
	r := productionRouterFor(t, cfg)

	var observed int
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/test") {
			t.Errorf("%s %s: the test-login backdoor is gone and no route may come back under /test", method, route)
		}
		if strings.HasPrefix(route, "/observe/") {
			observed++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	if observed == 0 {
		t.Fatal("walked no /observe route, so the router under test is not the production one")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/test/login", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /test/login = %d, want 404", rec.Code)
	}
}

var (
	backtickSpan   = regexp.MustCompile("`([^`\\n]+)`")
	httpMethodLead = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE) `)
)

func namedGoAPIRoutes(note string) []string {
	var routes []string
	for _, span := range backtickSpan.FindAllStringSubmatch(note, -1) {
		path, _, _ := strings.Cut(httpMethodLead.ReplaceAllString(span[1], ""), "?")
		isGoAPI := strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/observe/") ||
			strings.HasPrefix(path, "/admin/") || path == "/health"
		if isGoAPI {
			routes = append(routes, path)
		}
	}
	return routes
}

func routeMatchesPattern(route, pattern string) bool {
	routeSegments := strings.Split(route, "/")
	patternSegments := strings.Split(pattern, "/")
	if len(routeSegments) != len(patternSegments) {
		return false
	}
	for i, segment := range patternSegments {
		isParam := strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
		if !isParam && segment != routeSegments[i] {
			return false
		}
	}
	return true
}

func unmountedRoutes(note string, mounted []string) []string {
	var missing []string
	for _, route := range namedGoAPIRoutes(note) {
		found := false
		for _, pattern := range mounted {
			found = found || routeMatchesPattern(route, pattern)
		}
		if !found {
			missing = append(missing, route)
		}
	}
	return missing
}

func TestUnmountedRoutes(t *testing.T) {
	mounted := []string{"/health", "/v1/discovery/search", "/v1/tracks/{trackId}/retry", "/observe/metrics/live"}
	cases := []struct {
		name string
		note string
		want []string
	}{
		{"mounted route passes", "see `/v1/discovery/search`", nil},
		{"unmounted admin route is reported", "see `/admin/metrics/live`", []string{"/admin/metrics/live"}},
		{"concrete id matches a param segment", "`POST /v1/tracks/abc123/retry`", nil},
		{"query string is dropped", "`GET /observe/metrics/live?window_days=<n>&by=x`", nil},
		{"method prefix is dropped", "`GET /v1/discovery/gone`", []string{"/v1/discovery/gone"}},
		{"overseer route is ignored", "`/overseer/api/state`", nil},
		{"health matches exactly", "`/health` and `/healthz`", nil},
		{"longer route than pattern is a miss", "`/v1/discovery/search/extra`", []string{"/v1/discovery/search/extra"}},
		{"span outside backticks is ignored", "/admin/metrics/live", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := unmountedRoutes(c.note, mounted)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("unmountedRoutes(%q) = %v, want %v", c.note, got, c.want)
			}
		})
	}
}

func TestCapabilityNotes_NameMountedRoutes(t *testing.T) {
	var mounted []string
	err := chi.Walk(productionRouter(t), func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted = append(mounted, route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	notes, err := filepath.Glob("../../../../docs/features/*/notes.md")
	if err != nil {
		t.Fatalf("glob capability notes: %v", err)
	}
	for _, notePath := range notes {
		body, err := os.ReadFile(notePath)
		if err != nil {
			t.Fatalf("read %s: %v", notePath, err)
		}
		for _, route := range unmountedRoutes(string(body), mounted) {
			t.Errorf("%s: %s is not mounted", notePath, route)
		}
	}
}

func TestRouter_PublicAuthFailureRouteIsAnonymousAndLibraryStaysAuthed(t *testing.T) {
	r := assembledRouter(t, jwksConfig(t), dbtest.Pool(t))

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"reason":"invalid_credentials","app_version":"1.2.3"}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/public/auth-failures", body))
	if rec.Code != http.StatusNoContent {
		t.Errorf("POST /v1/public/auth-failures without Authorization = %d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/library", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/library without Authorization = %d, want 401", rec.Code)
	}
}

func TestRouter_EveryRouteButThePublicOnesAnswersAnAnonymousCallerWith401(t *testing.T) {
	r := productionRouter(t)
	type call struct{ method, path string }
	var calls []call
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		guarded := strings.HasPrefix(route, "/v1/") && !strings.HasPrefix(route, "/v1/public/") || strings.HasPrefix(route, "/observe/")
		if guarded {
			calls = append(calls, call{method, regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(route, "x")})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("walked no guarded route, so the router under test is not the production one")
	}
	for _, c := range calls {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s without Authorization = %d, want 401", c.method, c.path, rec.Code)
			}
		})
	}
}

func TestRouter_V1ResponsesCarryNosniffAndNoStore(t *testing.T) {
	r := assembledRouter(t, jwksConfig(t), dbtest.Pool(t))

	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"authed route rejected", http.MethodGet, "/v1/library", "", http.StatusUnauthorized},
		{"discovery per-user route rejected", http.MethodGet, "/v1/discovery/search-history", "", http.StatusUnauthorized},
		{"event stream rejected", http.MethodGet, "/v1/events", "", http.StatusUnauthorized},
		{"client events rejected", http.MethodPost, "/v1/telemetry/events", `{"type":"screen_view"}`, http.StatusUnauthorized},
		{"audio stream rejected", http.MethodGet, "/v1/tracks/abc/audio", "", http.StatusUnauthorized},
		{"audio recover rejected", http.MethodPost, "/v1/tracks/abc/audio/recover", "", http.StatusUnauthorized},
		{"playback rejected", http.MethodGet, "/v1/playback/queue-state", "", http.StatusUnauthorized},
		{"public route", http.MethodPost, "/v1/public/auth-failures", `{"reason":"invalid_credentials","app_version":"1.2.3"}`, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestRouter_OneClientsFailedTokensShareOneBudgetAcrossEveryV1Group(t *testing.T) {
	r := assembledRouter(t, jwksConfig(t), dbtest.Pool(t))
	const client = "198.51.100.77:4321"
	attempt := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = client
		req.Header.Set("Authorization", "Bearer not-a-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	const attempts = 200
	throttled := false
	for range attempts {
		if attempt("/v1/library") == http.StatusTooManyRequests {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Fatalf("%d failed tokens on a bounded route never ran out the failure budget", attempts)
	}

	for _, path := range []string{"/v1/events", "/v1/tracks/abc/audio"} {
		if got := attempt(path); got != http.StatusTooManyRequests {
			t.Errorf("GET %s after the client ran out its failure budget = %d, want 429 from the one shared throttle", path, got)
		}
	}
}

func TestRoutes_OnlyAWiredSchedulerMountsRetryAndReacquire(t *testing.T) {
	for _, tt := range []struct {
		name      string
		ytMusic   bool
		wantRoute bool
	}{
		{name: "no sources leaves acquisition unwired", ytMusic: false, wantRoute: false},
		{name: "a source wires the scheduler and its retry routes", ytMusic: true, wantRoute: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newJWKSFixture(t)
			f.cfg.YtMusicEnabled = tt.ytMusic
			h := assembledHandler(t, f.cfg)
			token := signedToken(t, f.key, f.operator)

			for _, action := range []string{"retry", "reacquire"} {
				req := httptest.NewRequest(http.MethodPost, "/v1/tracks/"+uuid.NewString()+"/"+action, nil)
				req.RemoteAddr = "192.0.2.1:1234"
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if mounted := rec.Code != http.StatusNotFound; mounted != tt.wantRoute {
					t.Errorf("POST %s answered %d: mounted = %v, want %v", action, rec.Code, mounted, tt.wantRoute)
				}
			}
		})
	}
}

func runUntilItReturns(t *testing.T, cfg *config.Config) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	return app.New(cfg, nil).Run(ctx)
}

func TestRun_AnUnreachableDatabaseStopsStartup(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.DatabaseURL = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"

	err := runUntilItReturns(t, cfg)

	if err == nil || !strings.HasPrefix(err.Error(), "database: ") {
		t.Fatalf("Run = %v, want the database error", err)
	}
}

func TestRun_AFailedAssemblyStopsStartup(t *testing.T) {
	dbtest.RequireIntegration(t)
	cfg := jwksConfig(t)
	cfg.DatabaseURL = dbtest.DatabaseURL(t)
	cfg.SupabaseJWTJWKSURL = "http://keys.example.com/jwks.json"

	err := runUntilItReturns(t, cfg)

	if err == nil || !strings.HasPrefix(err.Error(), "auth: ") {
		t.Fatalf("Run = %v, want the assembly's auth error", err)
	}
}

func TestRun_APortAlreadyInUseIsReturnedInsteadOfServing(t *testing.T) {
	dbtest.RequireIntegration(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = taken.Close() })
	cfg := jwksConfig(t)
	cfg.DatabaseURL = dbtest.DatabaseURL(t)
	cfg.Host = "127.0.0.1"
	cfg.Port = taken.Addr().(*net.TCPAddr).Port

	logs := logging.CaptureText(t, slog.LevelInfo)

	err = runUntilItReturns(t, cfg)

	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("Run = %v, want the listen error for the taken port", err)
	}
	if !strings.Contains(logs.String(), `msg="shutdown complete"`) {
		t.Errorf("Run returned the listen error without shutting the assembly down and closing the datastores; log:\n%s", logs.String())
	}
}
