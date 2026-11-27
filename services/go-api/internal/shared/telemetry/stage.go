package telemetry

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type stage struct {
	span  trace.Span
	now   clock.Func
	start time.Time
}

func StartStage(ctx context.Context, now clock.Func, name string, attrs ...attribute.KeyValue) (context.Context, *stage) {
	if now == nil {
		panic("telemetry.StartStage: a clock is required")
	}
	ctx, span := otel.Tracer(ScopeName).Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, &stage{span: span, now: now, start: now()}
}

func (s *stage) Annotate(attrs ...attribute.KeyValue) {
	s.span.SetAttributes(attrs...)
}

func (s *stage) End(err error) time.Duration {
	elapsed := s.now().Sub(s.start)
	if err != nil {
		s.span.RecordError(err)
		s.span.SetStatus(codes.Error, err.Error())
	}
	s.span.End()
	return elapsed
}
