package cleanup

import (
	"context"
	"log"
	"time"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

const batchLimit = 500

type Store interface {
	Delete(ctx context.Context, key string) error
}

type Repo interface {
	ExpiredArtifacts(ctx context.Context, cutoff time.Time, limit int) ([]db.ExpiredArtifact, error)
	DeleteArtifact(ctx context.Context, orgID int64, hash string) error
}

// RunOnce deletes one batch of expired artifacts (object first, then DB row).
func RunOnce(ctx context.Context, repo Repo, store Store, retention time.Duration) (int, error) {
	cutoff := time.Now().Add(-retention)
	rows, err := repo.ExpiredArtifacts(ctx, cutoff, batchLimit)
	if err != nil {
		return 0, err
	}
	var n int
	for _, a := range rows {
		key := turbo.StorageKey(a.OrgSlug, a.Hash)
		if err := store.Delete(ctx, key); err != nil {
			log.Printf("cleanup: storage delete %s failed, will retry: %v", key, err)
			continue // leave the DB row so it is retried next tick
		}
		if err := repo.DeleteArtifact(ctx, a.OrgID, a.Hash); err != nil {
			log.Printf("cleanup: db delete org=%d hash=%s failed: %v", a.OrgID, a.Hash, err)
			continue
		}
		n++
	}
	return n, nil
}

// Run ticks on a long-lived context (app lifecycle, not a request context)
// and drains one batch per tick until ctx is cancelled.
func Run(ctx context.Context, repo Repo, store Store, retention, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := RunOnce(ctx, repo, store, retention); err != nil {
				log.Printf("cleanup: batch failed: %v", err)
			} else if n > 0 {
				log.Printf("cleanup: removed %d expired artifacts", n)
			}
		}
	}
}

// ponytail: object-then-row; a mid-crash leaves a DB row with no object
// (self-heals — next tick's storage.Delete is a no-op and drops the row).
// The reverse (orphan object) is what we avoid.
