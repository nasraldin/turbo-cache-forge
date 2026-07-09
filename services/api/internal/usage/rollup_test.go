package usage

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu   sync.Mutex
	rows map[int64]Delta
}

func (f *fakeSink) AddUsage(_ context.Context, orgID int64, _ time.Time, up, down, hits, misses int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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
