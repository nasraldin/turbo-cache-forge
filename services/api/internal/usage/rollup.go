package usage

import (
	"context"
	"time"
)

type Sink interface {
	AddUsage(ctx context.Context, orgID int64, day time.Time, up, down, hits, misses int64) error
}

// Rollup drains the accumulator into the sink under today's UTC date.
// A per-org write failure does not drop that org's counters: the delta is
// re-absorbed into the accumulator so the next tick retries it, and Rollup
// keeps writing the remaining orgs rather than aborting the whole batch.
// The last error seen, if any, is returned for the caller to log.
func Rollup(ctx context.Context, acc *Accumulator, sink Sink) error {
	day := time.Now().UTC()
	var lastErr error
	for orgID, d := range acc.Drain() {
		if err := sink.AddUsage(ctx, orgID, day, d.Up, d.Down, d.Hits, d.Misses); err != nil {
			acc.Add(orgID, d)
			lastErr = err
			continue
		}
	}
	return lastErr
}

// Run rolls up on an interval until ctx is cancelled, then does a final drain.
func Run(ctx context.Context, acc *Accumulator, sink Sink, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = Rollup(context.Background(), acc, sink) // flush remaining on shutdown
			return
		case <-t.C:
			_ = Rollup(ctx, acc, sink)
		}
	}
}
