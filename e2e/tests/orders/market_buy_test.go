//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// O6 — a market buy spends a quote budget and reports itself filled.
//
// Rests an ask, then sends a market buy whose budget covers it, watching the private stream.
// Expect: it trades at the maker's price, the stream's terminal status is "filled" (not
// partially_filled), no cancelled remainder is recorded for the sub-quantum dust, and any
// unspendable dust is returned to the balance. Regression guard for the 2026-08 baseScale /
// dust fix, where such an order was reported cancelled despite getting all its liquidity.
func TestMarketBuySpendsBudgetAndReportsFilled(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price := band(t)
	available := minQty() * 2
	// A budget the resting ask can absorb entirely, plus a sliver that buys nothing.
	budget := env.Market.Notional(price, available) + 1

	rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, available))

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, taker.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the taker's order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, taker.LoginToken)
	orderID := send(t, ctx, taker, fixtures.MarketBuy(env.Market, budget))

	ev := assert.StreamStatus(t, ctx, events, orderID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled)
	if ev.Status != stream.StatusFilled {
		t.Fatalf("market buy reported %q, want %q — it received all the liquidity it could afford",
			ev.Status, stream.StatusFilled)
	}

	order := assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, orderID)
	assert.Traded(t, order)
	if got := order.Matches[0].Price; got != price {
		t.Fatalf("traded at %d, want the maker's price %d", got, price)
	}
	if order.CancelledOrder != nil {
		t.Fatalf("dust left over from a satisfied market buy was recorded as a cancellation: %+v",
			*order.CancelledOrder)
	}

	moved := diffAgainst(t, ctx, taker.LoginToken, before)
	if got := moved[env.Market.QuoteSymbol].Blocked; got != 0 {
		t.Fatalf("taker still has %d %s blocked, want the dust released", got, env.Market.QuoteSymbol)
	}
	if got := moved[env.Market.BaseSymbol]; got.Balance <= 0 {
		t.Fatalf("taker received %d %s, want a positive credit", got.Balance, env.Market.BaseSymbol)
	}
	if spent := -moved[env.Market.QuoteSymbol].Net(); spent > int64(budget) {
		t.Fatalf("taker gave up %d %s, more than its %d budget", spent, env.Market.QuoteSymbol, budget)
	}
}

// O7 — a market buy the book cannot absorb is partially filled, not silently dropped.
//
// Sends a market buy whose budget far exceeds the single resting ask.
// Expect: it takes everything available, the unspent budget is recorded as a cancelled
// remainder and returned to the balance, and the stream reports partially_filled.
func TestMarketBuyAgainstThinBookIsPartiallyFilled(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price := band(t)
	available := minQty()
	budget := env.Market.Notional(price, available) * 5

	rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, available))

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, taker.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the taker's order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, taker.LoginToken)
	orderID := send(t, ctx, taker, fixtures.MarketBuy(env.Market, budget))

	ev := assert.StreamStatus(t, ctx, events, orderID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled)
	if ev.Status != stream.StatusPartiallyFilled {
		t.Fatalf("market buy against a thin book reported %q, want %q",
			ev.Status, stream.StatusPartiallyFilled)
	}

	order := assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, orderID)
	assert.Traded(t, order)
	if got := order.Matches[0].BaseAmount; got != available {
		t.Fatalf("bought %d base, want the %d the book held", got, available)
	}

	remainder := assert.Cancelled(t, order)
	if remainder.RemainingHave == 0 {
		t.Fatal("no unspent budget recorded, but most of it could not be spent")
	}

	moved := diffAgainst(t, ctx, taker.LoginToken, before)
	if got := moved[env.Market.QuoteSymbol].Blocked; got != 0 {
		t.Fatalf("taker still has %d %s blocked, want the unspent budget released",
			got, env.Market.QuoteSymbol)
	}
	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price)
}
