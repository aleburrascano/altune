package telemetry_test

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"altune/go-api/internal/shared/telemetry/internal/adapters/otlp"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
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
	if otlp.Enabled(noEndpoint) {
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
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(never)))
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

func TestRequestSpanNamesTheSpanByRouteAndRecordsTheApp(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(never)))
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

func TestRequestSpanTagsTheSpanWithTheCorrelationIDWhenOneIsSet(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(logging.ContextWithCorrelationID(r.Context(), "corr-abc")))
		})
	})
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(never)))
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

func TestRequestSpanLeavesTheCorrelationAttributeOffWhenNoneIsSet(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(never)))
	router.Get("/v1/ping", func(http.ResponseWriter, *http.Request) {})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/ping", nil))

	for _, kv := range recorder.Ended()[0].Attributes() {
		if string(kv.Key) == "corr_id" {
			t.Fatalf("corr_id = %q on a request with no correlation id, want the attribute absent", kv.Value.String())
		}
	}
}

func TestRequestSpanMarksServerErrorsAndUnmatchedRoutes(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(never)))
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

func TestRequestSpanSkipsTheRequestsItIsToldTo(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(func(r *http.Request) bool { return r.URL.Path == "/health" })))
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

func TestClientEvents_CarriesTheCorrelationIdWhenGiven(t *testing.T) {
	_, attrs := emitOne(t, "", telemetry.ClientEvent{Type: "x", CorrelationID: "corr-1"})
	if got := attrs["correlation.id"].AsString(); got != "corr-1" {
		t.Errorf("correlation.id = %q, want corr-1", got)
	}
	_, none := emitOne(t, "", telemetry.ClientEvent{Type: "x"})
	if _, ok := none["correlation.id"]; ok {
		t.Error("correlation.id must be absent when empty")
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

func TestStartStageRecordsAChildSpanOfTheRequestAndReturnsTheClockElapsed(t *testing.T) {
	recorder := recordSpans(t)
	manual := clocktest.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var elapsed time.Duration
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(clock.Now, telemetry.RequestSpan(never)))
	router.Get("/v1/search", func(_ http.ResponseWriter, r *http.Request) {
		_, stage := telemetry.StartStage(r.Context(), manual.Func(), "provider.search", attribute.String("provider", "deezer"))
		manual.Advance(1500 * time.Millisecond)
		stage.Annotate(attribute.String("provider.status", "ok"))
		elapsed = stage.End(nil)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/search", nil))

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want the stage and the request", len(spans))
	}
	stage, request := spans[0], spans[1]
	if stage.Name() != "provider.search" || stage.Parent().SpanID() != request.SpanContext().SpanID() {
		t.Fatalf("stage = %q with parent %v, want provider.search under the request span", stage.Name(), stage.Parent().SpanID())
	}
	got := map[string]string{}
	for _, kv := range stage.Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	if got["provider"] != "deezer" || got["provider.status"] != "ok" {
		t.Fatalf("attributes = %v", got)
	}
	if elapsed != 1500*time.Millisecond {
		t.Fatalf("elapsed = %v, want 1.5s from the injected clock", elapsed)
	}
}

func TestStageEndMarksTheSpanAsAnErrorWhenTheStageFailed(t *testing.T) {
	recorder := recordSpans(t)
	_, stage := telemetry.StartStage(context.Background(), clock.Now, "download.full")

	stage.End(errors.New("source refused"))

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error || spans[0].Status().Description != "source refused" {
		t.Fatalf("spans = %+v, want one span marked as an error with the cause", spans)
	}
}

func TestStartStageRefusesANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("StartStage(nil clock) did not panic")
		}
	}()
	telemetry.StartStage(context.Background(), nil, "x")
}

func TestRegistryHandsBackTheSameInstrumentForTheSameName(t *testing.T) {
	reg := telemetry.NewRegistry()

	reg.Counter("requests_total").Add(2)
	reg.Counter("requests_total").Add(3)
	reg.Gauge("breaker_open").Set(1)
	reg.LabeledCounter("rejections").Add("expired", 1)
	reg.LabeledCounter("rejections").Add("expired", 1)
	reg.LabeledCounter("rejections").Add("forged", 1)

	if got := reg.Counter("requests_total").Value(); got != 5 {
		t.Errorf("counter = %d, want 5", got)
	}
	if got := reg.Gauge("breaker_open").Value(); got != 1 {
		t.Errorf("gauge = %d, want 1", got)
	}
	if got := reg.LabeledCounter("rejections").Values(); got["expired"] != 2 || got["forged"] != 1 || len(got) != 2 {
		t.Errorf("labeled = %v, want expired 2 and forged 1", got)
	}
	if reg.Counter("a") == reg.Counter("b") {
		t.Error("two names share one counter")
	}
}

func TestHistogramBucketsAnObservationAtOrBelowItsBound(t *testing.T) {
	h := telemetry.NewRegistry().Histogram("latency_ms", []int64{10, 100})

	for _, v := range []int64{3, 10, 11, 100, 101, -5} {
		h.Observe(v)
	}

	snap := h.Snapshot()
	if snap.Count != 6 || snap.Sum != 3+10+11+100+101 {
		t.Errorf("count = %d, sum = %d, want 6 and 225", snap.Count, snap.Sum)
	}
	if want := []uint64{3, 2, 1}; snap.Buckets[0] != want[0] || snap.Buckets[1] != want[1] || snap.Buckets[2] != want[2] {
		t.Errorf("buckets = %v, want %v", snap.Buckets, want)
	}
}

func TestNewHistogramRefusesBoundsThatAreNotAscending(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("descending bounds did not panic")
		}
	}()
	telemetry.NewHistogram([]int64{100, 10})
}

func TestRegistryIsSafeUnderConcurrentUse(t *testing.T) {
	reg := telemetry.NewRegistry()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				reg.Counter("c").Add(1)
				reg.LabeledCounter("l").Add("x", 1)
			}
		}()
	}
	wg.Wait()

	if got := reg.Counter("c").Value(); got != 1600 {
		t.Errorf("counter = %d, want 1600", got)
	}
}
