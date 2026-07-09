package usage

import "sync"

type Delta struct {
	Up, Down, Hits, Misses int64
}

type Accumulator struct {
	mu sync.Mutex
	m  map[int64]*Delta
}

func New() *Accumulator { return &Accumulator{m: map[int64]*Delta{}} }

func (a *Accumulator) at(orgID int64) *Delta {
	d := a.m[orgID]
	if d == nil {
		d = &Delta{}
		a.m[orgID] = d
	}
	return d
}

func (a *Accumulator) Hit(orgID, bytesDown int64) {
	a.mu.Lock()
	d := a.at(orgID)
	d.Hits++
	d.Down += bytesDown
	a.mu.Unlock()
}

func (a *Accumulator) Miss(orgID int64) {
	a.mu.Lock()
	a.at(orgID).Misses++
	a.mu.Unlock()
}

func (a *Accumulator) Upload(orgID, bytesUp int64) {
	a.mu.Lock()
	d := a.at(orgID)
	d.Up += bytesUp
	a.mu.Unlock()
}

// Drain returns accumulated deltas and resets. Values are copied out under lock.
// ponytail: one global mutex; contention is a non-issue at 4 int64 adds per cache op. Shard only if a profile ever says so.
func (a *Accumulator) Drain() map[int64]Delta {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[int64]Delta, len(a.m))
	for id, d := range a.m {
		out[id] = *d
	}
	a.m = map[int64]*Delta{}
	return out
}
