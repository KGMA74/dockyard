// Package tracing wires up OpenTelemetry distributed tracing. It is entirely
// opt-in: Init is a no-op (global tracer stays the OTel default no-op
// implementation) unless OTEL_EXPORTER_OTLP_ENDPOINT is set — no other config
// surface exists on purpose, so operators who don't want tracing see zero
// overhead and zero new knobs.
package tracing

import (
	"context"
	"log/slog"
	"os"
	"time"

	"dockyard/internal/version"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

// storageTracer is a package-level handle to the global tracer provider.
// Before Init runs (or when tracing is never enabled) otel's default
// TracerProvider is a no-op, so StartSpan calls elsewhere in the codebase
// are always safe to make unconditionally — they cost a couple of no-op
// interface calls when disabled.
var storageTracer = otel.Tracer("dockyard/storage")

// StartSpan starts a span for a storage backend operation, named after it
// (e.g. "GetBlob", "PutManifest"). Callers should defer the returned end
// func. A no-op when tracing is disabled.
func StartSpan(ctx context.Context, op string, attrs ...attribute.KeyValue) (context.Context, func(err error)) {
	ctx, span := storageTracer.Start(ctx, "storage."+op, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}
}

// Init configures the global OTel tracer provider from the standard
// OTEL_EXPORTER_OTLP_ENDPOINT env var. Returns a shutdown func to flush and
// close the exporter on server shutdown, and false if tracing was left
// disabled (endpoint unset) — callers should skip installing otelecho in
// that case to avoid any per-request overhead.
func Init(ctx context.Context) (shutdown func(context.Context) error, enabled bool) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return func(context.Context) error { return nil }, false
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		slog.Error("tracing: failed to create OTLP exporter", "err", err)
		return func(context.Context) error { return nil }, false
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("dockyard"),
			semconv.ServiceVersion(version.Version),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("tracing enabled", "endpoint", endpoint)

	return func(shutdownCtx context.Context) error {
		ctx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, true
}
