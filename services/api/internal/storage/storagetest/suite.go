package storagetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

// Run exercises the contract every Storage backend must satisfy.
func Run(t *testing.T, newStore func() storage.Storage) {
	t.Helper()
	ctx := context.Background()

	t.Run("put/get/head roundtrip", func(t *testing.T) {
		s := newStore()
		want := []byte("hello-artifact")
		if err := s.Put(ctx, "team-a/h1", bytes.NewReader(want)); err != nil {
			t.Fatal(err)
		}
		info, err := s.Head(ctx, "team-a/h1")
		if err != nil || info.Size != int64(len(want)) {
			t.Fatalf("Head = %+v, %v", info, err)
		}
		rc, _, err := s.Get(ctx, "team-a/h1")
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		got, _ := io.ReadAll(rc)
		if !bytes.Equal(got, want) {
			t.Fatalf("Get = %q", got)
		}
	})

	t.Run("missing → ErrNotFound", func(t *testing.T) {
		s := newStore()
		if _, err := s.Head(ctx, "team-a/missing"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		s := newStore()
		_ = s.Put(ctx, "team-a/del", bytes.NewReader([]byte("x")))
		if err := s.Delete(ctx, "team-a/del"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Head(ctx, "team-a/del"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("after delete want ErrNotFound, got %v", err)
		}
	})
}
