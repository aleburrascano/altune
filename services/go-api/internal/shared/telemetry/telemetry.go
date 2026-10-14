package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	EndpointEnv   = "OTEL_EXPORTER_OTLP_ENDPOINT"
	ScopeName     = "altune/go-api"
	exportTimeout = 3 * time.Second
	retryGiveUp   = 10 * time.Second
)

type Service struct {
	Name        string
	Version     string
	Environment string
}

type Shutdown func(context.Context) error

func Enabled() bool {
	return os.Getenv(EndpointEnv) != ""
}

func Setup(ctx context.Context, svc Service) (Shutdown, error) {
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}
	res := describeService(svc)
	tracer, err := newTracerProvider(ctx, res)
	if err != nil {
		return nil, err
	}
	logger, err := newLoggerProvider(ctx, res)
	if err != nil {
		return nil, errors.Join(err, tracer.Shutdown(ctx))
	}
	otel.SetTracerProvider(tracer)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetLoggerProvider(logger)
	return func(ctx context.Context) error {
		return errors.Join(tracer.Shutdown(ctx), logger.Shutdown(ctx))
	}, nil
}

func describeService(svc Service) *resource.Resource {
	return resource.NewSchemaless(
		semconv.ServiceName(svc.Name),
		semconv.ServiceVersion(svc.Version),
		semconv.DeploymentEnvironment(svc.Environment),
	)
}

func newTracerProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	spans, err := otlptracehttp.New(ctx,
		otlptracehttp.WithTimeout(exportTimeout),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: true, InitialInterval: time.Second, MaxInterval: 2 * time.Second, MaxElapsedTime: retryGiveUp}),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}
	return sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(spans)), nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	records, err := otlploghttp.New(ctx,
		otlploghttp.WithTimeout(exportTimeout),
		otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: true, InitialInterval: time.Second, MaxInterval: 2 * time.Second, MaxElapsedTime: retryGiveUp}),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp log exporter: %w", err)
	}
	return sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(records))), nil
}
