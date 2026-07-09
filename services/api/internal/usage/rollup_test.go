package usage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu      sync.Mutex
	rows    map[int64]Delta
	failFor map[int64]bool
}

func (f *fakeSink) AddUsage(_ context.Context, orgID int64, _ time.Time, up, down, hits, misses int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFor[orgID] {
		return fmt.Errorf("simulated write failure for org %d", orgID)
	}
	if f.rows == nil {
		f.rows = map[int64]Delta{}
	}
	f.rows[orgID] = Delta{Up: up, Down: down, Hits: hits, Misses: misses}
	return nil
}

func TestAccumulateAndRollup(t *testing.T) {
	acc := New()
	acc.Hit(7, 100)
	acc.Hit(7, 50)
	acc.Miss(7)
	acc.Upload(7, 200)
	acc.Hit(9, 10)

	sink := &fakeSink{}
	if err := Rollup(context.Background(), acc, sink); err != nil {
		t.Fatal(err)
	}
	if got := sink.rows[7]; got.Hits != 2 || got.Misses != 1 || got.Down != 150 || got.Up != 200 {
		t.Fatalf("org 7 rollup = %+v", got)
	}
	if got := sink.rows[9]; got.Hits != 1 || got.Down != 10 {
		t.Fatalf("org 9 rollup = %+v", got)
	}
	// drained: a second rollup with no new activity writes nothing
	sink2 := &fakeSink{}
	_ = Rollup(context.Background(), acc, sink2)
	if len(sink2.rows) != 0 {
		t.Fatalf("expected empty after drain, got %+v", sink2.rows)
	}
}

// TestRollupReabsorbsOnPartialFailure proves a transient per-org write error
// no longer drops that org's counters: they must survive back in the
// accumulator for the next tick to retry, while orgs that succeeded are
// still written and Rollup still reports the error to the caller.
func TestRollupReabsorbsOnPartialFailure(t *testing.T) {
	acc := New()
	acc.Hit(1, 100)
	acc.Upload(1, 20)
	acc.Hit(2, 10)
	acc.Miss(2)

	sink := &fakeSink{failFor: map[int64]bool{2: true}}
	err := Rollup(context.Background(), acc, sink)
	if err == nil {
		t.Fatal("expected Rollup to return an error when a per-org write fails")
	}

	if got := sink.rows[1]; got.Hits != 1 || got.Down != 100 || got.Up != 20 {
		t.Fatalf("org 1 should have been written: %+v", got)
	}
	if _, ok := sink.rows[2]; ok {
		t.Fatalf("org 2 should not have been written by the fake sink")
	}

	// New activity for org 2 arrives before the retry.
	acc.Hit(2, 5)

	drained := acc.Drain()
	got, ok := drained[2]
	if !ok {
		t.Fatal("org 2's delta was dropped instead of being re-absorbed for retry")
	}
	if got.Hits != 2 || got.Misses != 1 || got.Down != 15 {
		t.Fatalf("org 2 re-absorbed delta should merge with post-failure activity: %+v", got)
	}
	if _, ok := drained[1]; ok {
		t.Fatalf("org 1 should have been drained already, not re-absorbed")
	}
}

func TestConcurrentAccumulate(t *testing.T) {
	acc := New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); acc.Hit(1, 1) }()
	}
	wg.Wait()
	if d := acc.Drain()[1]; d.Hits != 100 || d.Down != 100 {
		t.Fatalf("race lost updates: %+v", d)
	}
}
