package assert

import (
	"context"
	"fmt"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// Most of the suite infers an order's state from which legs are present (open_order /
// cancelled_order / matches); the persisted status is also on the response, and is the only
// way to see a pending bracket exit, which has no leg at all until it fires or is cancelled.

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

// EventuallyResting polls until orderID is readable and sitting in the book, and returns it.
// POST /orders only queues the order, so this is how a test waits for the matching engine to
// have accepted and rested it.
func EventuallyResting(t testing.TB, ctx context.Context, c *client.Client, token, orderID string) client.Order {
	t.Helper()
	return eventuallyOrder(t, ctx, c, token, orderID, func(o client.Order) error {
		if o.OpenOrder == nil {
			return fmt.Errorf("order %s is not resting (%s)", o.ID, describe(o))
		}
		return nil
	})
}

// EventuallyNotResting polls until orderID has left the book (filled, cancelled, or expired),
// and returns it.
func EventuallyNotResting(t testing.TB, ctx context.Context, c *client.Client, token, orderID string) client.Order {
	t.Helper()
	return eventuallyOrder(t, ctx, c, token, orderID, func(o client.Order) error {
		if o.OpenOrder != nil {
			return fmt.Errorf("order %s is still resting: %+v", o.ID, *o.OpenOrder)
		}
		return nil
	})
}

// Pending asserts o is a parked bracket exit: status pending, nothing resting, nothing
// cancelled, nothing traded.
func Pending(t testing.TB, o client.Order) {
	t.Helper()
	if o.Status != client.StatusPending || o.OpenOrder != nil || o.CancelledOrder != nil || len(o.Matches) != 0 {
		t.Fatalf("assert.Pending: order %s is %q (%s), want a pending exit", o.ID, o.Status, describe(o))
	}
}

// EventuallyStatus polls until orderID is readable with one of the given statuses, and
// returns it. It is the completion signal for a bracket exit, which is never "resting" — it
// goes from pending straight to filled / partially_filled / cancelled.
func EventuallyStatus(t testing.TB, ctx context.Context, c *client.Client, token, orderID string, statuses ...string) client.Order {
	t.Helper()
	return eventuallyOrder(t, ctx, c, token, orderID, func(o client.Order) error {
		for _, s := range statuses {
			if o.Status == s {
				return nil
			}
		}
		return fmt.Errorf("order %s is %q, want one of %v", o.ID, o.Status, statuses)
	})
}

func eventuallyOrder(t testing.TB, ctx context.Context, c *client.Client, token, orderID string, ok func(client.Order) error) client.Order {
	t.Helper()

	var order client.Order
	Eventually(t, ctx, func() error {
		o, err := c.GetOrder(ctx, token, orderID)
		if err != nil {
			return err
		}
		if err := ok(o); err != nil {
			return err
		}
		order = o
		return nil
	})
	return order
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
