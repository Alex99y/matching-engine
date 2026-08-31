//go:build e2e

package marketdata

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// M2 — the depth snapshot mirrors the book, in price order.
//
// Rests three buys at descending prices and one sell above them.
// Expect: each level reports exactly the resting quantity, bids come back best-first
// (high→low) and asks best-first (low→high), and the best bid never crosses the best ask.
func TestDepthReflectsTheBookInPriceOrder(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	tick := env.Market.PriceQuantum
	top := env.Band(t)
	qty := env.MinQty()

	bids := []uint64{top - tick, top - 2*tick, top - 3*tick}
	ask := top

	for _, price := range bids {
		env.Rest(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty))
	}
	env.Rest(t, ctx, acc, fixtures.LimitSell(env.Market, ask, qty))

	for _, price := range bids {
		assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price, qty)
	}
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, ask, qty)

	depth, err := env.Client.GetDepth(ctx, env.Market.Ref, 0)
	if err != nil {
		t.Fatalf("get depth: %v", err)
	}
	if depth.Market != env.Market.Ref {
		t.Fatalf("depth is for market %q, want %q", depth.Market, env.Market.Ref)
	}

	assertDescending(t, "bids", depth.Bids)
	assertAscending(t, "asks", depth.Asks)

	if len(depth.Bids) > 0 && len(depth.Asks) > 0 && depth.Bids[0].Price >= depth.Asks[0].Price {
		t.Fatalf("book is crossed: best bid %d >= best ask %d — these should have matched",
			depth.Bids[0].Price, depth.Asks[0].Price)
	}
	for _, side := range [][]client.DepthLevel{depth.Bids, depth.Asks} {
		for _, l := range side {
			if l.Quantity == 0 {
				t.Fatalf("depth reports an empty level at price %d; emptied levels must be dropped", l.Price)
			}
		}
	}
}

// M2 — grouping buckets levels without inventing or losing quantity.
//
// Rests three buys on adjacent ticks, then reads the book grouped into a bucket wide enough
// to hold all three.
// Expect: the ungrouped read shows them separately, the grouped read collapses them into one
// bucket carrying the summed quantity, and that bucket's price is on the grouping grid.
func TestDepthGroupingSumsAdjacentLevels(t *testing.T) {
	if env.Market.PriceQuantum == 0 {
		t.Skip("market has no price grid to group by")
	}

	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	tick := env.Market.PriceQuantum
	group := 4 * tick
	// Anchor to the grouping grid so all three ticks land in one bucket.
	top := (env.Band(t) / group) * group
	if top < group {
		t.Skip("price band is too low to group")
	}
	prices := []uint64{top, top + tick, top + 2*tick}
	qty := env.MinQty()

	for _, price := range prices {
		env.Rest(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty))
	}
	for _, price := range prices {
		assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price, qty)
	}

	grouped, err := env.Client.GetDepth(ctx, env.Market.Ref, group)
	if err != nil {
		t.Fatalf("get grouped depth: %v", err)
	}

	total := uint64(len(prices)) * qty
	if got := assert.LevelQty(grouped, client.Buy, top); got != total {
		t.Fatalf("grouped bucket at %d holds %d, want %d (the three ticks summed)", top, got, total)
	}
	for _, l := range grouped.Bids {
		if l.Price%group != 0 {
			t.Fatalf("grouped level %d is not on the %d grid", l.Price, group)
		}
	}
}

func assertDescending(t *testing.T, side string, levels []client.DepthLevel) {
	t.Helper()
	for i := 1; i < len(levels); i++ {
		if levels[i-1].Price <= levels[i].Price {
			t.Fatalf("%s are not ordered best-first: %d then %d", side, levels[i-1].Price, levels[i].Price)
		}
	}
}

func assertAscending(t *testing.T, side string, levels []client.DepthLevel) {
	t.Helper()
	for i := 1; i < len(levels); i++ {
		if levels[i-1].Price >= levels[i].Price {
			t.Fatalf("%s are not ordered best-first: %d then %d", side, levels[i-1].Price, levels[i].Price)
		}
	}
}
