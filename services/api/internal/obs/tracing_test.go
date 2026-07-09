package obs

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitTracerNoopWhenEndpointUnset(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := InitTracer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown should never error, got %v", err)
	}

	_, span := StartStorageSpan(context.Background(), "test-span")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Fatal("expected an invalid (no-op) span context when tracing is off")
	}
}

func TestInitTracerRegistersRealProviderWhenEndpointSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := InitTracer(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, span := StartStorageSpan(context.Background(), "test-span")
	if !span.SpanContext().IsValid() {
		t.Fatal("expected a valid span context once a real TracerProvider is registered — export failures to an unreachable collector happen async and must not affect this")
	}
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = shutdown(ctx) // best-effort; nothing is actually listening on :4318 in this test
}
