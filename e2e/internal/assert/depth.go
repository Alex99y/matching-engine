package assert

import (
	"context"
	"fmt"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// LevelQty returns the resting quantity at price on the given side of d, or 0 if that level
// is absent.
func LevelQty(d client.Depth, side client.OrderSide, price uint64) uint64 {
	levels := d.Bids
	if side == client.Sell {
		levels = d.Asks
	}
	for _, l := range levels {
		if l.Price == price {
			return l.Quantity
		}
	}
	return 0
}

// HasLevel asserts the book holds exactly qty at price on side.
func HasLevel(t testing.TB, d client.Depth, side client.OrderSide, price, qty uint64) {
	t.Helper()
	if got := LevelQty(d, side, price); got != qty {
		t.Fatalf("assert.HasLevel: %s @ %d = %d, want %d", side, price, got, qty)
	}
}

// NoLevel asserts nothing rests at price on side.
func NoLevel(t testing.TB, d client.Depth, side client.OrderSide, price uint64) {
	t.Helper()
	if got := LevelQty(d, side, price); got != 0 {
		t.Fatalf("assert.NoLevel: %s @ %d = %d, want absent", side, price, got)
	}
}

// EventuallyLevel polls GET /markets/{m}/depth until price holds exactly qty on side. The
// depth endpoint is served from a cache the API syncs off core's event stream, so it trails
// the order by a moment even after the order itself reads back as resting.
func EventuallyLevel(t testing.TB, ctx context.Context, c *client.Client, market string, side client.OrderSide, price, qty uint64) {
	t.Helper()
	Eventually(t, ctx, func() error {
		d, err := c.GetDepth(ctx, market, 0)
		if err != nil {
			return err
		}
		if got := LevelQty(d, side, price); got != qty {
			return fmt.Errorf("%s @ %d = %d, want %d", side, price, got, qty)
		}
		return nil
	})
}

// EventuallyNoLevel polls until nothing rests at price on side.
func EventuallyNoLevel(t testing.TB, ctx context.Context, c *client.Client, market string, side client.OrderSide, price uint64) {
	t.Helper()
	EventuallyLevel(t, ctx, c, market, side, price, 0)
}
