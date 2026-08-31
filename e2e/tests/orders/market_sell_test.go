//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O8 — a market sell offers a base quantity and takes whatever bids are there.
//
// Rests a bid, then sells that exact quantity at market.
// Expect: it fills against the resting bid at the bid's price, leaves nothing in the book,
// records no cancelled remainder, and the seller's base leaves while quote arrives.
func TestMarketSellFillsAgainstBids(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	seller := env.NewFundedAccount(t)

	price, qty := band(t), minQty()

	rest(t, ctx, maker, fixtures.LimitBuy(env.Market, price, qty))
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price, qty)

	before := snapshot(t, ctx, seller.LoginToken)
	orderID := send(t, ctx, seller, fixtures.MarketSell(env.Market, qty))

	order := assert.EventuallyNotResting(t, ctx, env.Client, seller.LoginToken, orderID)
	assert.Traded(t, order)
	if got := order.Matches[0].Price; got != price {
		t.Fatalf("traded at %d, want the resting bid's price %d", got, price)
	}
	if got := order.Matches[0].BaseAmount; got != qty {
		t.Fatalf("sold %d base, want %d", got, qty)
	}
	if order.CancelledOrder != nil {
		t.Fatalf("a fully filled market sell recorded a cancelled remainder: %+v", *order.CancelledOrder)
	}

	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price)

	moved := diffAgainst(t, ctx, seller.LoginToken, before)
	if got := moved[env.Market.BaseSymbol]; got.Net() != -int64(qty) {
		t.Fatalf("seller's %s net movement = %d, want -%d", env.Market.BaseSymbol, got.Net(), qty)
	}
	if got := moved[env.Market.QuoteSymbol]; got.Balance <= 0 {
		t.Fatalf("seller received %d %s, want a positive credit", got.Balance, env.Market.QuoteSymbol)
	}
	if got := moved[env.Market.BaseSymbol].Blocked; got != 0 {
		t.Fatalf("seller still has %d %s blocked after a market sell, want 0", got, env.Market.BaseSymbol)
	}
}
