package filesystem

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type FS struct{ root string }

func New(root string) *FS { return &FS{root: root} }

// path resolves key under root and refuses to escape it.
func (f *FS) path(key string) (string, error) {
	// Reject keys with absolute paths or directory traversal attempts
	if strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return "", storage.ErrNotFound
	}
	full := filepath.Join(f.root, key)
	// Verify the resolved path is still within root
	rel, err := filepath.Rel(f.root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", storage.ErrNotFound
	}
	return full, nil
}

func (f *FS) Put(_ context.Context, key string, r io.Reader) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p) // atomic publish
}

func (f *FS) Get(_ context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, &storage.ObjectInfo{Size: st.Size()}, nil
}

func (f *FS) Head(_ context.Context, key string) (*storage.ObjectInfo, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if os.IsNotExist(err) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &storage.ObjectInfo{Size: st.Size()}, nil
}

func (f *FS) Delete(_ context.Context, key string) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
