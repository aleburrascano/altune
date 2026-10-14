package telemetry

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	AppVersionHeader = "X-Altune-App-Version"
	PlatformHeader   = "X-Altune-Platform"
	AnalyticsHeader  = "X-Altune-Analytics"
	AnalyticsOff     = "off"
	maxHeaderValue   = 64
	unmatchedRoute   = "unmatched"
)

func Middleware(skip func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			serveTraced(next, w, r)
		})
	}
}

func serveTraced(next http.Handler, w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(ScopeName).Start(r.Context(), r.Method, trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	next.ServeHTTP(ww, r.WithContext(ctx))
	describe(span, r, ww.Status())
}

func describe(span trace.Span, r *http.Request, status int) {
	if status == 0 {
		status = http.StatusOK
	}
	route := routePattern(r)
	span.SetName(r.Method + " " + route)
	span.SetAttributes(
		attribute.String("http.request.method", r.Method),
		attribute.String("http.route", route),
		attribute.Int("http.response.status_code", status),
		attribute.String("app.version", Clip(r.Header.Get(AppVersionHeader))),
		attribute.String("app.platform", Clip(r.Header.Get(PlatformHeader))),
	)
	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
}

func AnalyticsAllowed(r *http.Request) bool {
	return r.Header.Get(AnalyticsHeader) != AnalyticsOff
}

func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.RoutePattern() == "" {
		return unmatchedRoute
	}
	return rctx.RoutePattern()
}

func Clip(value string) string {
	if len(value) > maxHeaderValue {
		return value[:maxHeaderValue]
	}
	return value
}
