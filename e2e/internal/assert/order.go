package assert

import (
	"context"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// The REST order response has no status field — the suite infers state from which legs are
// present (open_order / cancelled_order / matches). The live stream carries the real status.

// Resting asserts o is still in the book and returns its open leg.
func Resting(t testing.TB, o client.Order) client.OrderLeg {
	t.Helper()
	if o.OpenOrder == nil {
		t.Fatalf("assert.Resting: order %s is not resting (%s)", o.ID, describe(o))
	}
	return *o.OpenOrder
}

// NotResting asserts o has left the book (filled or terminal).
func NotResting(t testing.TB, o client.Order) {
	t.Helper()
	if o.OpenOrder != nil {
		t.Fatalf("assert.NotResting: order %s still resting: %+v", o.ID, *o.OpenOrder)
	}
}

// Cancelled asserts o carries a cancelled-remainder leg and returns it. Note a partially
// filled IOC/market order also has one (for the unfilled part).
func Cancelled(t testing.TB, o client.Order) client.OrderLeg {
	t.Helper()
	if o.CancelledOrder == nil {
		t.Fatalf("assert.Cancelled: order %s has no cancelled leg (%s)", o.ID, describe(o))
	}
	return *o.CancelledOrder
}

// Traded asserts o has at least one fill.
func Traded(t testing.TB, o client.Order) {
	t.Helper()
	if len(o.Matches) == 0 {
		t.Fatalf("assert.Traded: order %s has no matches (%s)", o.ID, describe(o))
	}
}

// StreamStatus waits for the next event for orderID on s to reach one of want, failing the
// test on timeout or stream error. Returns the matching event.
func StreamStatus(t testing.TB, ctx context.Context, s *stream.UserStream, orderID string, want ...string) stream.OrderEvent {
	t.Helper()
	ev, err := s.WaitForStatus(ctx, orderID, want...)
	if err != nil {
		t.Fatalf("assert.StreamStatus: order %s waiting for %v: %v", orderID, want, err)
	}
	return ev
}

func describe(o client.Order) string {
	switch {
	case o.OpenOrder != nil:
		return "resting"
	case o.CancelledOrder != nil && len(o.Matches) > 0:
		return "partially filled, remainder cancelled"
	case o.CancelledOrder != nil:
		return "cancelled, never traded"
	default:
		return "filled"
	}
}
