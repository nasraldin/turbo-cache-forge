package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

type fakeStore struct {
	deleted []string
	err     error
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return f.err
}

type fakeRepo struct {
	expired   []db.ExpiredArtifact
	dbDeletes []string
}

func (f *fakeRepo) ExpiredArtifacts(context.Context, time.Time, int) ([]db.ExpiredArtifact, error) {
	out := f.expired
	f.expired = nil // second call returns none (drained)
	return out, nil
}
func (f *fakeRepo) DeleteArtifact(_ context.Context, orgID int64, hash string) error {
	f.dbDeletes = append(f.dbDeletes, hash)
	return nil
}

func TestRunOnceDeletesObjectThenRow(t *testing.T) {
	store := &fakeStore{}
	repo := &fakeRepo{expired: []db.ExpiredArtifact{{OrgID: 7, OrgSlug: "org-test", Hash: "old"}}}

	n, err := RunOnce(context.Background(), repo, store, 30*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
	wantKey := turbo.StorageKey("org-test", "old")
	if len(store.deleted) != 1 || store.deleted[0] != wantKey {
		t.Fatalf("storage deleted = %v, want [%s]", store.deleted, wantKey)
	}
	if len(repo.dbDeletes) != 1 || repo.dbDeletes[0] != "old" {
		t.Fatalf("db deleted = %v", repo.dbDeletes)
	}
}

// If the storage delete fails, the DB row must NOT be deleted so the next
// tick retries it (row stays discoverable via ExpiredArtifacts).
func TestRunOnceSkipsDBDeleteWhenStorageDeleteFails(t *testing.T) {
	store := &fakeStore{err: errors.New("s3 unavailable")}
	repo := &fakeRepo{expired: []db.ExpiredArtifact{{OrgID: 7, OrgSlug: "org-test", Hash: "old"}}}

	n, err := RunOnce(context.Background(), repo, store, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if n != 0 {
		t.Fatalf("RunOnce n = %d, want 0", n)
	}
	if len(store.deleted) != 1 {
		t.Fatalf("storage delete attempted = %v, want 1 call", store.deleted)
	}
	if len(repo.dbDeletes) != 0 {
		t.Fatalf("db deleted = %v, want none (row must survive for retry)", repo.dbDeletes)
	}
}

// storage.Delete is idempotent: fs/s3 backends treat a missing object as
// success (nil error), so a re-run against an already-deleted object still
// drops the DB row instead of leaving it stuck forever.
func TestRunOnceIdempotentWhenObjectAlreadyGone(t *testing.T) {
	store := &fakeStore{} // nil error, mimics "already gone" success
	repo := &fakeRepo{expired: []db.ExpiredArtifact{{OrgID: 7, OrgSlug: "org-test", Hash: "old"}}}

	n, err := RunOnce(context.Background(), repo, store, 30*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
	if len(repo.dbDeletes) != 1 || repo.dbDeletes[0] != "old" {
		t.Fatalf("db deleted = %v, want row dropped despite missing object", repo.dbDeletes)
	}
}
