package filesystem

import (
	"bytes"
	"context"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/storagetest"
)

func TestFilesystemConformance(t *testing.T) {
	storagetest.Run(t, func() storage.Storage { return New(t.TempDir()) })
}

func TestRejectsPathTraversal(t *testing.T) {
	fs := New(t.TempDir())
	err := fs.Put(context.Background(), "../../etc/passwd", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
}
