package storage

import (
	"context"
	"errors"
	"io"
)

type ObjectInfo struct{ Size int64 }

type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error)
	Head(ctx context.Context, key string) (*ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

var ErrNotFound = errors.New("storage: object not found")
