package httpx

import (
	"context"
	"time"
)

// gate serialises the requests to one paced host and keeps at least gap
// between the end of one and the start of the next.
type gate struct {
	gap time.Duration
	// slot is a one-slot semaphore: a select on it lets a waiting request
	// give up when its context is cancelled, which sync.Mutex does not.
	slot chan struct{}
	// last is when the previous request finished; guarded by slot.
	last time.Time
}

func newGate(gap time.Duration) *gate {
	return &gate{gap: gap, slot: make(chan struct{}, 1)}
}

// acquire waits for the host's turn and for the gap to pass. On success the
// caller must call release once its request has finished.
func (g *gate) acquire(ctx context.Context, now func() time.Time, sleep func(context.Context, time.Duration) error) error {
	select {
	case g.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !g.last.IsZero() {
		if d := g.last.Add(g.gap).Sub(now()); d > 0 {
			if err := sleep(ctx, d); err != nil {
				<-g.slot
				return err
			}
		}
	}
	return nil
}

// release records when the request finished and lets the next one in.
func (g *gate) release(now func() time.Time) {
	g.last = now()
	<-g.slot
}
