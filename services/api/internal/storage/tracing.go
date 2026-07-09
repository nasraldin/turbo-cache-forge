package storage

import (
	"context"
	"io"

	"go.opentelemetry.io/otel/attribute"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

type tracingStorage struct{ inner Storage }

// WithTracing wraps any Storage backend so every call emits a span — a
// no-op span, and effectively free, unless obs.InitTracer has registered a
// real TracerProvider. Apply this once in main.go regardless of which
// backend (fs or s3) is configured; both get tracing with no
// backend-specific instrumentation to keep in sync.
func WithTracing(inner Storage) Storage { return &tracingStorage{inner: inner} }

func (t *tracingStorage) Put(ctx context.Context, key string, r io.Reader) error {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Put")
	span.SetAttributes(attribute.String("cache.key", key))
	err := t.inner.Put(ctx, key, r)
	obs.EndSpan(span, err)
	return err
}

func (t *tracingStorage) Get(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Get")
	span.SetAttributes(attribute.String("cache.key", key))
	rc, info, err := t.inner.Get(ctx, key)
	obs.EndSpan(span, err)
	return rc, info, err
}

func (t *tracingStorage) Head(ctx context.Context, key string) (*ObjectInfo, error) {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Head")
	span.SetAttributes(attribute.String("cache.key", key))
	info, err := t.inner.Head(ctx, key)
	obs.EndSpan(span, err)
	return info, err
}

func (t *tracingStorage) Delete(ctx context.Context, key string) error {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Delete")
	span.SetAttributes(attribute.String("cache.key", key))
	err := t.inner.Delete(ctx, key)
	obs.EndSpan(span, err)
	return err
}
