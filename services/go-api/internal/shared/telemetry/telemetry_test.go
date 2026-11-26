package telemetry_test

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetupWithoutEndpointIsANoOp(t *testing.T) {
	noEndpoint := func(string) string { return "" }
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Service{Name: "altune-api", Version: "abc123", Environment: "test"}, noEndpoint)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if telemetry.Enabled(noEndpoint) {
		t.Fatal("Enabled() = true with no endpoint")
	}
}

func TestADownCollectorNeitherFailsNorSlowsARequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := listener.Addr().String()
	_ = listener.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+closedAddr)
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Service{Name: "altune-api", Version: "abc123", Environment: "test"}, os.Getenv)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	router := chi.NewRouter()
	router.Use(telemetry.Middleware(never))
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	start := time.Now()
	for range 50 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("50 requests took %v with the collector down", elapsed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = shutdown(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not return after its context expired")
	}
}

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

func TestMiddlewareNamesTheSpanByRouteAndRecordsTheApp(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(telemetry.Middleware(never))
	router.Get("/v1/tracks/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks/42?q=secret+search", nil)
	req.Header.Set(telemetry.AppVersionHeader, "1.4.0+"+strings.Repeat("a", 100))
	req.Header.Set(telemetry.PlatformHeader, "ios")
	router.ServeHTTP(httptest.NewRecorder(), req)
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name() != "GET /v1/tracks/{id}" {
		t.Fatalf("name = %q", span.Name())
	}
	got := map[string]string{}
	for _, kv := range span.Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	if got["http.route"] != "/v1/tracks/{id}" || got["http.response.status_code"] != "404" || got["app.platform"] != "ios" {
		t.Fatalf("attributes = %v", got)
	}
	if len(got["app.version"]) != 64 {
		t.Fatalf("app.version not clipped: %q", got["app.version"])
	}
	for key, value := range got {
		if strings.Contains(value, "secret") || strings.Contains(value, "42") {
			t.Fatalf("attribute %s leaks the path or query: %q", key, value)
		}
	}
	if span.Status().Code == codes.Error {
		t.Fatal("a 404 marked the span as an error")
	}
}

func TestMiddlewareTagsTheSpanWithTheCorrelationIDWhenOneIsSet(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(logging.ContextWithCorrelationID(r.Context(), "corr-abc")))
		})
	})
	router.Use(telemetry.Middleware(never))
	router.Get("/v1/ping", func(http.ResponseWriter, *http.Request) {})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/ping", nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	got := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	if got["corr_id"] != "corr-abc" {
		t.Fatalf("attributes = %v, want corr_id corr-abc", got)
	}
}

func TestMiddlewareLeavesTheCorrelationAttributeOffWhenNoneIsSet(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(telemetry.Middleware(never))
	router.Get("/v1/ping", func(http.ResponseWriter, *http.Request) {})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/ping", nil))

	for _, kv := range recorder.Ended()[0].Attributes() {
		if string(kv.Key) == "corr_id" {
			t.Fatalf("corr_id = %q on a request with no correlation id, want the attribute absent", kv.Value.String())
		}
	}
}

func TestMiddlewareMarksServerErrorsAndUnmatchedRoutes(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(telemetry.Middleware(never))
	router.Get("/boom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nowhere/123", nil))
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatal("a 502 was not marked as an error")
	}
	if spans[1].Name() != "GET unmatched" {
		t.Fatalf("unmatched name = %q", spans[1].Name())
	}
}

func never(*http.Request) bool { return false }

func TestMiddlewareSkipsTheRequestsItIsToldTo(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(telemetry.Middleware(func(r *http.Request) bool { return r.URL.Path == "/health" }))
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || len(recorder.Ended()) != 0 {
		t.Fatalf("status %d, spans %d; want 200 and no span", rec.Code, len(recorder.Ended()))
	}
}

type recordingLogger struct {
	embedded.Logger
	records []log.Record
}

func (l *recordingLogger) Emit(_ context.Context, r log.Record) { l.records = append(l.records, r) }
func (l *recordingLogger) Enabled(context.Context, log.EnabledParameters) bool {
	return true
}

func attrMap(r log.Record) map[string]attribute.Value {
	out := map[string]attribute.Value{}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value
		return true
	})
	return out
}

func emitOne(t *testing.T, key string, e telemetry.ClientEvent) (log.Record, map[string]attribute.Value) {
	t.Helper()
	l := &recordingLogger{}
	telemetry.NewClientEvents(clock.Now, key, l).Emit(context.Background(), e)
	if len(l.records) != 1 {
		t.Fatalf("records = %d, want 1", len(l.records))
	}
	return l.records[0], attrMap(l.records[0])
}

func TestClientEvents_ScrubsPayload(t *testing.T) {
	rec, attrs := emitOne(t, "", telemetry.ClientEvent{
		Type: "search_submitted", EventID: "e1", AppVersion: "1.2.3", Platform: "ios",
		Payload: map[string]any{
			"title": "Daft Punk", "message": "boom", "surface": "search", "position": float64(3),
			"will_retry": true, "results": []any{1, 2}, "session_id": "s-1",
		},
	})
	if rec.Body().AsString() != "client_event" || rec.Severity() != log.SeverityInfo {
		t.Errorf("body/severity = %q/%v", rec.Body().AsString(), rec.Severity())
	}
	for _, k := range []string{"payload.surface", "payload.position", "payload.will_retry", "event.type", "event.id", "app.version", "app.platform", "session.id"} {
		if _, ok := attrs[k]; !ok {
			t.Errorf("missing attribute %s", k)
		}
	}
	for _, k := range []string{"payload.title", "payload.message", "payload.results"} {
		if _, ok := attrs[k]; ok {
			t.Errorf("attribute %s must be dropped", k)
		}
	}
}

func TestClientEvents_StringsOutsideAllowListDropped(t *testing.T) {
	_, attrs := emitOne(t, "", telemetry.ClientEvent{Type: "x", Payload: map[string]any{"query": "secret", "error": "e", "session_id": "s"}})
	for k := range attrs {
		if strings.HasPrefix(k, "payload.") {
			t.Errorf("unexpected attribute %s", k)
		}
	}
}

func TestClientEvents_OmitsEmptyEventID(t *testing.T) {
	_, attrs := emitOne(t, "", telemetry.ClientEvent{Type: "x"})
	if _, ok := attrs["event.id"]; ok {
		t.Error("event.id must be absent when empty")
	}
}

func TestClientEvents_PseudonymisesUser(t *testing.T) {
	_, attrs := emitOne(t, "k", telemetry.ClientEvent{Type: "x", UserID: "u-123"})
	id := attrs["user.pseudo_id"].AsString()
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Errorf("user.pseudo_id = %q, want 16 hex chars", id)
	}
	for k, v := range attrs {
		if strings.Contains(k, "u-123") || strings.Contains(v.String(), "u-123") {
			t.Errorf("attribute %s leaks the raw user id", k)
		}
	}
	_, again := emitOne(t, "k", telemetry.ClientEvent{Type: "x", UserID: "u-123"})
	if again["user.pseudo_id"].AsString() != id {
		t.Error("pseudo id must be stable")
	}
}

func TestClientEvents_NoKeyNoUserAttribute(t *testing.T) {
	_, attrs := emitOne(t, "", telemetry.ClientEvent{Type: "x", UserID: "u-123"})
	for k, v := range attrs {
		if strings.HasPrefix(k, "user.") || strings.Contains(v.String(), "u-123") {
			t.Errorf("unexpected user attribute %s", k)
		}
	}
}

func TestNewClientEvents_RefusesAMissingClock(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "NewClientEvents") {
			t.Errorf("panic = %q, want a refusal naming NewClientEvents", msg)
		}
	}()
	telemetry.NewClientEvents(nil, "k", nil)
}
