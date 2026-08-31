//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O2 — a taker that fully consumes one maker settles both sides.
//
// Rests a sell, then buys the same quantity at the same price from a second account.
// Expect: one match at the maker's price, both orders leave the book fully filled, the base
// and quote legs move in opposite directions for the two accounts, and — summed across both —
// each asset's net movement equals exactly the negative of the fees charged in it.
func TestFullCrossSettlesBothSides(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	notional := env.Market.Notional(price, qty)

	makerBefore := snapshot(t, ctx, maker.LoginToken)
	takerBefore := snapshot(t, ctx, taker.LoginToken)

	makerID := rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, qty))
	takerID := send(t, ctx, taker, fixtures.LimitBuy(env.Market, price, qty))

	// Both sides are done once neither is in the book any more.
	assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, takerID)
	assert.EventuallyNotResting(t, ctx, env.Client, maker.LoginToken, makerID)
	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price)

	makerOrder := fetch(t, ctx, maker.LoginToken, makerID)
	takerOrder := fetch(t, ctx, taker.LoginToken, takerID)

	for name, o := range map[string]client.Order{"maker": makerOrder, "taker": takerOrder} {
		assert.Traded(t, o)
		if len(o.Matches) != 1 {
			t.Fatalf("%s has %d matches, want 1", name, len(o.Matches))
		}
		m := o.Matches[0]
		if m.Price != price || m.BaseAmount != qty || m.QuoteAmount != notional {
			t.Fatalf("%s match: price=%d base=%d quote=%d, want %d/%d/%d",
				name, m.Price, m.BaseAmount, m.QuoteAmount, price, qty, notional)
		}
		// Neither order has an unfilled remainder to cancel.
		if o.CancelledOrder != nil {
			t.Fatalf("%s recorded a cancelled remainder on a full fill: %+v", name, *o.CancelledOrder)
		}
	}
	if takerOrder.Matches[0].IsTaker != true {
		t.Fatal("the crossing order is not marked as the taker")
	}
	if makerOrder.Matches[0].IsTaker != false {
		t.Fatal("the resting order is marked as the taker")
	}

	// Buyer pays quote and receives base; seller does the reverse.
	makerMoved := diffAgainst(t, ctx, maker.LoginToken, makerBefore)
	takerMoved := diffAgainst(t, ctx, taker.LoginToken, takerBefore)

	if got := takerMoved[env.Market.BaseSymbol]; got.Balance <= 0 {
		t.Fatalf("buyer received %d %s, want a positive credit", got.Balance, env.Market.BaseSymbol)
	}
	if got := makerMoved[env.Market.QuoteSymbol]; got.Balance <= 0 {
		t.Fatalf("seller received %d %s, want a positive credit", got.Balance, env.Market.QuoteSymbol)
	}
	if got := takerMoved[env.Market.QuoteSymbol]; got.Net() >= 0 {
		t.Fatalf("buyer's %s net movement = %d, want negative (it spent quote)", env.Market.QuoteSymbol, got.Net())
	}
	if got := makerMoved[env.Market.BaseSymbol]; got.Net() >= 0 {
		t.Fatalf("seller's %s net movement = %d, want negative (it gave up base)", env.Market.BaseSymbol, got.Net())
	}

	// Nothing was created or destroyed beyond the fees each side paid.
	assert.Conserved(t, env.Market.BaseSymbol, env.Market.QuoteSymbol,
		[]map[string]assert.Move{makerMoved, takerMoved}, makerOrder, takerOrder)
}
