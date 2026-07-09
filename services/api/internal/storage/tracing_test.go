package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

type fakeStore struct{}

func (f *fakeStore) Put(context.Context, string, io.Reader) error { return nil }
func (f *fakeStore) Get(context.Context, string) (io.ReadCloser, *ObjectInfo, error) {
	return io.NopCloser(bytes.NewReader(nil)), &ObjectInfo{}, nil
}
func (f *fakeStore) Head(context.Context, string) (*ObjectInfo, error) { return &ObjectInfo{}, nil }
func (f *fakeStore) Delete(context.Context, string) error              { return nil }

func TestWithTracingEmitsSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	traced := WithTracing(&fakeStore{})
	_ = traced.Put(context.Background(), "team-a/h1", bytes.NewReader([]byte("x")))
	_, _, _ = traced.Get(context.Background(), "team-a/h1")
	_, _ = traced.Head(context.Background(), "team-a/h1")
	_ = traced.Delete(context.Background(), "team-a/h1")

	spans := exp.GetSpans()
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want 4 (Put/Get/Head/Delete)", len(spans))
	}
	seen := map[string]bool{}
	for _, s := range spans {
		seen[s.Name] = true
	}
	for _, want := range []string{"storage.Put", "storage.Get", "storage.Head", "storage.Delete"} {
		if !seen[want] {
			t.Errorf("missing span %q", want)
		}
	}
}
