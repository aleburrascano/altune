package telemetry

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetupWithoutEndpointIsANoOp(t *testing.T) {
	t.Setenv(EndpointEnv, "")
	shutdown, err := Setup(context.Background(), Service{Name: "altune-api", Version: "abc123", Environment: "test"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if Enabled() {
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
	t.Setenv(EndpointEnv, "http://"+closedAddr)
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	shutdown, err := Setup(context.Background(), Service{Name: "altune-api", Version: "abc123", Environment: "test"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	router := chi.NewRouter()
	router.Use(Middleware(never))
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
	router.Use(Middleware(never))
	router.Get("/v1/tracks/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks/42?q=secret+search", nil)
	req.Header.Set(AppVersionHeader, "1.4.0+"+strings.Repeat("a", 100))
	req.Header.Set(PlatformHeader, "ios")
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
	if len(got["app.version"]) != maxHeaderValue {
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

func TestMiddlewareMarksServerErrorsAndUnmatchedRoutes(t *testing.T) {
	recorder := recordSpans(t)
	router := chi.NewRouter()
	router.Use(Middleware(never))
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
	router.Use(Middleware(func(r *http.Request) bool { return r.URL.Path == "/health" }))
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || len(recorder.Ended()) != 0 {
		t.Fatalf("status %d, spans %d; want 200 and no span", rec.Code, len(recorder.Ended()))
	}
}

func TestAnalyticsAllowedUnlessTheAppSaysOff(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/discovery/events", nil)
	if !AnalyticsAllowed(req) {
		t.Fatal("no header should allow analytics")
	}
	req.Header.Set(AnalyticsHeader, AnalyticsOff)
	if AnalyticsAllowed(req) {
		t.Fatal("the off header should stop analytics")
	}
}
