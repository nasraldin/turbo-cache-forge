package usage

import (
	"context"
	"time"
)

type Sink interface {
	AddUsage(ctx context.Context, orgID int64, day time.Time, up, down, hits, misses int64) error
}

// Rollup drains the accumulator into the sink under today's UTC date.
func Rollup(ctx context.Context, acc *Accumulator, sink Sink) error {
	day := time.Now().UTC()
	for orgID, d := range acc.Drain() {
		if err := sink.AddUsage(ctx, orgID, day, d.Up, d.Down, d.Hits, d.Misses); err != nil {
			return err
		}
	}
	return nil
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
