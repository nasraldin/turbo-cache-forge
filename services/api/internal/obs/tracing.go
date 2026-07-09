package obs

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer wires a real OTLP/HTTP tracer provider when
// OTEL_EXPORTER_OTLP_ENDPOINT is set; otherwise it does nothing, leaving
// otel's built-in no-op global TracerProvider in place so every
// StartStorageSpan/StartDBSpan call elsewhere is a true zero-cost no-op by
// default. This env var is the ONLY tracing on/off switch.
func InitTracer(ctx context.Context) (shutdown func(context.Context) error, err error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	exp, err := otlptracehttp.New(ctx) // reads OTEL_EXPORTER_OTLP_* env vars itself
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(),
		resource.NewSchemaless(attribute.String("service.name", "turbo-cache-forge")))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// ponytail: look up the tracer fresh on every call instead of caching it in
// a package var. otel's global Tracer forwards to whatever TracerProvider is
// registered *at Start time* only if it's asked fresh each time; a
// package-level `var t = otel.Tracer(...)` obtained before any
// SetTracerProvider call binds to otel's internal one-time delegate swap and
// never notices a later SetTracerProvider (observed: breaks span validity
// across tests that call SetTracerProvider more than once in one process).
func StartStorageSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("turbo-cache-forge/storage").Start(ctx, name)
}

func StartDBSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("turbo-cache-forge/db").Start(ctx, name)
}

// EndSpan records err (if any) and ends span. Called via defer at every span
// call site so storage/db instrumentation doesn't repeat this dance.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
