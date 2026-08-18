package correlate

import (
	"context"

	"github.com/alex99y/matching-engine/loadtest/internal/stream"
)

// Pump reads events from a single stream until it closes or ctx is done, feeding each one to
// every tracker. This is how one UserStream connection serves multiple trackers watching for
// different conditions on the same order flow — e.g. the cancel scenario's full-lifecycle and
// round-trip spans, which both resolve on the same "cancelled" event but from different sentAt
// baselines.
func Pump(ctx context.Context, events <-chan stream.OrderEvent, trackers ...*Tracker) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			for _, t := range trackers {
				t.Feed(event)
			}
		}
	}
}
