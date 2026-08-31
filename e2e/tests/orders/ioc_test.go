//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O4 — an IOC limit that finds nothing to trade against is cancelled, never rested.
//
// Sends a buy priced below everything in the book with time-in-force IOC.
// Expect: no fills, nothing at that price level, the order recorded as a cancellation for its
// full size, and the reservation released so the balance is exactly as it started.
func TestIOCWithoutLiquidityIsCancelled(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	notional := env.Market.Notional(price, qty)

	before := snapshot(t, ctx, acc.LoginToken)

	orderID := send(t, ctx, acc,
		fixtures.LimitBuy(env.Market, price, qty, fixtures.WithTIF(client.IOC)))

	order := assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, orderID)
	if len(order.Matches) != 0 {
		t.Fatalf("an IOC order with no liquidity traded: %+v", order.Matches)
	}

	remainder := assert.Cancelled(t, order)
	if remainder.RemainingHave != notional || remainder.RemainingWant != qty {
		t.Fatalf("cancelled remainder have=%d want=%d, expected the full %d/%d",
			remainder.RemainingHave, remainder.RemainingWant, notional, qty)
	}

	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price)

	open, err := env.Client.ListOrders(ctx, acc.LoginToken, client.OrdersFilter{Market: env.Market.Ref, ShowOpen: true})
	if err != nil {
		t.Fatalf("list open orders: %v", err)
	}
	if containsOrder(open, orderID) {
		t.Fatal("an IOC order came to rest in the book")
	}

	moved := diffAgainst(t, ctx, acc.LoginToken, before)
	if got := moved[env.Market.QuoteSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after a killed IOC: balance=%d blocked=%d, want 0/0",
			env.Market.QuoteSymbol, got.Balance, got.Blocked)
	}
}

// O4 — an IOC that crosses takes what it can and cancels the rest.
//
// Rests one lot, then sends an IOC buy for three at that price.
// Expect: one lot trades, the two it could not fill are recorded as a cancelled remainder
// rather than resting, and the unspent reservation comes back.
func TestIOCFillsWhatItCanThenCancels(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price := band(t)
	available := minQty()
	wanted := minQty() * 3

	rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, available))

	before := snapshot(t, ctx, taker.LoginToken)
	orderID := send(t, ctx, taker,
		fixtures.LimitBuy(env.Market, price, wanted, fixtures.WithTIF(client.IOC)))

	order := assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, orderID)
	assert.Traded(t, order)
	if got := order.Matches[0].BaseAmount; got != available {
		t.Fatalf("filled %d base, want the %d that was available", got, available)
	}

	remainder := assert.Cancelled(t, order)
	if want := wanted - available; remainder.RemainingWant != want {
		t.Fatalf("cancelled remainder wants %d base, want %d", remainder.RemainingWant, want)
	}

	// The unfillable two thirds were never spent, so the taker keeps that quote.
	moved := diffAgainst(t, ctx, taker.LoginToken, before)
	spent := -moved[env.Market.QuoteSymbol].Net()
	if maxSpend := int64(env.Market.Notional(price, available)); spent > maxSpend {
		t.Fatalf("taker gave up %d %s, more than the %d the single fill could cost",
			spent, env.Market.QuoteSymbol, maxSpend)
	}
	if got := moved[env.Market.QuoteSymbol].Blocked; got != 0 {
		t.Fatalf("taker still has %d %s blocked after an IOC, want 0", got, env.Market.QuoteSymbol)
	}
}
