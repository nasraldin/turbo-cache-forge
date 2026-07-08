package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

func TestPutGetHead(t *testing.T) {
	fs := New(t.TempDir())
	ctx := context.Background()
	want := []byte("artifact-bytes")

	if err := fs.Put(ctx, "team-a/abc123", bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	info, err := fs.Head(ctx, "team-a/abc123")
	if err != nil || info.Size != int64(len(want)) {
		t.Fatalf("Head = %+v, %v", info, err)
	}
	rc, _, err := fs.Get(ctx, "team-a/abc123")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, want) {
		t.Fatalf("Get = %q, want %q", got, want)
	}
}

func TestMissingIsErrNotFound(t *testing.T) {
	fs := New(t.TempDir())
	if _, err := fs.Head(context.Background(), "nope/x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Head missing = %v, want ErrNotFound", err)
	}
}

func TestRejectsPathTraversal(t *testing.T) {
	fs := New(t.TempDir())
	err := fs.Put(context.Background(), "../../etc/passwd", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
}
