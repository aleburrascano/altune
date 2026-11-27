package otlp

import (
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	AppVersionHeader = "X-Altune-App-Version"
	PlatformHeader   = "X-Altune-Platform"
	maxHeaderValue   = 64
)

func RequestSpan(skip func(*http.Request) bool) httpserver.Observer {
	return func(r *http.Request) (*http.Request, func(httpserver.Outcome)) {
		if skip(r) {
			return r, nil
		}
		ctx, span := otel.Tracer(ScopeName).Start(r.Context(), r.Method, trace.WithSpanKind(trace.SpanKindServer))
		r = r.WithContext(ctx)
		return r, func(o httpserver.Outcome) {
			describe(span, r, o)
			span.End()
		}
	}
}

func describe(span trace.Span, r *http.Request, o httpserver.Outcome) {
	span.SetName(o.Method + " " + o.Route)
	span.SetAttributes(
		attribute.String("http.request.method", o.Method),
		attribute.String("http.route", o.Route),
		attribute.Int("http.response.status_code", o.Status),
		attribute.String("app.version", clip(r.Header.Get(AppVersionHeader))),
		attribute.String("app.platform", clip(r.Header.Get(PlatformHeader))),
	)
	if corrID := logging.CorrelationIDFromContext(r.Context()); corrID != "" {
		span.SetAttributes(attribute.String(logging.KeyCorrelationID, corrID))
	}
	if o.Status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(o.Status))
	}
}

func clip(value string) string {
	if len(value) > maxHeaderValue {
		return value[:maxHeaderValue]
	}
	return value
}
