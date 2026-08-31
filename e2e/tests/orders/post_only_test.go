//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O9 — a post-only order that does not cross rests like any other GTC limit.
//
// Rests an ask, then sends a post-only buy priced below it.
// Expect: it comes to rest at its own price with its full quantity, taking nothing from the
// ask above it.
func TestPostOnlyRestsWhenItDoesNotCross(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	poster := env.NewFundedAccount(t)

	askPrice := band(t)
	bidPrice := askPrice - env.Market.PriceQuantum
	qty := minQty()

	makerID := rest(t, ctx, maker, fixtures.LimitSell(env.Market, askPrice, qty))

	orderID := send(t, ctx, poster,
		fixtures.LimitBuy(env.Market, bidPrice, qty, fixtures.PostOnly()))

	order := assert.EventuallyResting(t, ctx, env.Client, poster.LoginToken, orderID)
	if len(order.Matches) != 0 {
		t.Fatalf("a post-only order that does not cross traded: %+v", order.Matches)
	}
	if leg := assert.Resting(t, order); leg.Price != bidPrice {
		t.Fatalf("rested at %d, want %d", leg.Price, bidPrice)
	}

	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, bidPrice, qty)

	// The ask it was priced under is untouched.
	assert.Resting(t, fetch(t, ctx, maker.LoginToken, makerID))
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, askPrice, qty)
}

// O10 — a post-only order that would take liquidity is refused instead.
//
// Rests an ask, then sends a post-only buy priced at that ask.
// Expect: no fill, nothing rested at that price, the order recorded as cancelled for its full
// size with the reservation released, and the ask still sitting there untouched. Regression
// guard for the post-only feature: crossing must cancel, never trade.
func TestPostOnlyIsCancelledWhenItWouldCross(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	poster := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	notional := env.Market.Notional(price, qty)

	makerID := rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, qty))

	before := snapshot(t, ctx, poster.LoginToken)
	// Priced exactly at the ask: marketable, so post-only must refuse it.
	orderID := send(t, ctx, poster,
		fixtures.LimitBuy(env.Market, price, qty, fixtures.PostOnly()))

	order := assert.EventuallyNotResting(t, ctx, env.Client, poster.LoginToken, orderID)
	if len(order.Matches) != 0 {
		t.Fatalf("a crossing post-only order traded: %+v", order.Matches)
	}

	remainder := assert.Cancelled(t, order)
	if remainder.RemainingHave != notional || remainder.RemainingWant != qty {
		t.Fatalf("cancelled remainder have=%d want=%d, expected the untouched %d/%d",
			remainder.RemainingHave, remainder.RemainingWant, notional, qty)
	}

	// Nothing of the poster's is in the book, and the reservation came back.
	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price)
	if got := diffAgainst(t, ctx, poster.LoginToken, before)[env.Market.QuoteSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after a refused post-only: balance=%d blocked=%d, want 0/0",
			env.Market.QuoteSymbol, got.Balance, got.Blocked)
	}

	// The liquidity it would have taken is still on the book.
	assert.Resting(t, fetch(t, ctx, maker.LoginToken, makerID))
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price, qty)
}
