package router_test

import (
	"altune/go-api/internal/app/router"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/slowclient"
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	productionWriteTimeout   = 60 * time.Second
	productionRequestTimeout = 50 * time.Second
)

const routeWriteDeadline = 150 * time.Millisecond

func newRouter(s router.Settings) *chi.Mux {
	if s.WriteTimeout == 0 {
		s.WriteTimeout = productionWriteTimeout
	}
	if s.RequestTimeout == 0 {
		s.RequestTimeout = productionRequestTimeout
	}
	s.Latency = requestmetrics.NewRegistry()
	return router.New(s)
}

func TestRouter_PanickingHandlerLogsOneErrorAndRequestCompleteAt500(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

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

func newDeadlineRouter(uid ids.UserId, sse *eventstream.EventStreamHandler, bulkErrs chan<- error) *chi.Mux {
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
	r.Handle("/v1/events", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sse.ServeHTTP(w, req.WithContext(auth.ContextWithUserID(req.Context(), uid)))
	}))
	return r
}

func TestRouter_WriteDeadlineCutsOffSlowReaderOnNonSSERoute(t *testing.T) {
	bulkErrs := make(chan error, 1)
	sse := eventstream.NewEventStreamHandler(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), 0)
	srv := slowclient.NewServer(t, newDeadlineRouter(ids.NewUserId(uuid.New()), sse, bulkErrs))

	began := time.Now()
	slowclient.Get(t, srv, "/v1/bulk", nil)

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
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	sse := eventstream.NewEventStreamHandler(bus, 0).WithHeartbeat(routeWriteDeadline / 3)
	srv := slowclient.NewServer(t, newDeadlineRouter(uid, sse, make(chan error, 1)))

	conn, br := slowclient.Get(t, srv, "/v1/events", nil)
	resp := slowclient.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := bufio.NewReader(resp.Body)

	time.Sleep(4 * routeWriteDeadline)
	bus.Publish(context.Background(), uid, "late.event", map[string]any{"k": "v"})
	readUntil(t, body, func(l string) bool { return strings.HasPrefix(l, "event: late.event") })
}

func TestRouter_SSEPerFrameDeadlineReachesConnection(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	slog.SetDefault(slog.New(slog.DiscardHandler))

	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	sse := eventstream.NewEventStreamHandler(bus, 0).WithWriteTimeout(routeWriteDeadline).WithHeartbeat(time.Hour)
	done := make(chan struct{})
	router := newDeadlineRouter(uid, sse, make(chan error, 1))
	srv := slowclient.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		router.ServeHTTP(w, r)
	}))

	slowclient.Get(t, srv, "/v1/events", nil)

	payload := map[string]any{"pad": strings.Repeat("x", 16<<10)}
	giveUp := time.After(5 * time.Second)
	for {
		select {
		case <-done:
			return
		case <-giveUp:
			t.Fatal("SSE handler still blocked on a stalled client after 5s: per-frame deadline not applied")
		default:
			bus.Publish(context.Background(), uid, "bulk", payload)
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestRouter_RequestTimeoutReleasesBlockedV1HandlerButNotLongLivedRoutes(t *testing.T) {
	const budget = 100 * time.Millisecond
	r := newRouter(router.Settings{RequestTimeout: budget})
	blockUntilDone := func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
		case <-time.After(3 * budget):
			w.WriteHeader(http.StatusOK)
		}
	}
	r.Get("/v1/blocked", blockUntilDone)
	r.Get("/v1/events", blockUntilDone)
	r.Get("/v1/tracks/{trackId}/audio", blockUntilDone)
	r.Get("/health", blockUntilDone)

	cases := []struct {
		path string
		want int
	}{
		{"/v1/blocked", http.StatusGatewayTimeout},
		{"/v1/events", http.StatusOK},
		{"/v1/tracks/abc/audio", http.StatusOK},
		{"/health", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.want {
				t.Errorf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
			}
		})
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
	r := router.New(router.Settings{WriteTimeout: time.Minute, RequestTimeout: time.Minute, Latency: requestmetrics.NewRegistry()})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	r.Get("/health", ok)
	r.Get("/v1/events", ok)
	r.Get("/v1/tracks/{id}/audio", ok)
	r.Post("/v1/tracks/{id}/audio", ok)
	r.Get("/v1/library", ok)

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

func TestRouter_RecordsEachRequestsLatencyByRouteAndStatusClass(t *testing.T) {
	latency := requestmetrics.NewRegistry()
	r := router.New(router.Settings{WriteTimeout: time.Minute, RequestTimeout: time.Minute, Latency: latency})
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
