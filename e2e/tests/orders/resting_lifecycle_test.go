//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O1 — a GTC limit order rests until it is cancelled.
//
// Places a non-crossing limit buy, then cancels it.
// Expect: it appears in GET /orders?show_open and in the book at its price with its full
// quantity; after the cancel it is gone from both, is listed under ?show_cancelled with the
// unfilled remainder, and every quote unit it had blocked is returned to the balance.
func TestLimitOrderRestsThenCancels(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	notional := env.Market.Notional(price, qty)

	before := snapshot(t, ctx, acc.LoginToken)

	orderID := send(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty))

	// --- resting ---
	order := assert.EventuallyResting(t, ctx, env.Client, acc.LoginToken, orderID)
	leg := assert.Resting(t, order)
	if leg.Price != price {
		t.Fatalf("resting price = %d, want %d", leg.Price, price)
	}
	if leg.Side != string(client.Buy) {
		t.Fatalf("resting side = %q, want %q", leg.Side, client.Buy)
	}
	// A buy holds quote and wants base.
	if leg.RemainingHave != notional || leg.RemainingWant != qty {
		t.Fatalf("resting remainder have=%d want=%d, expected %d/%d", leg.RemainingHave, leg.RemainingWant, notional, qty)
	}

	open, err := env.Client.ListOrders(ctx, acc.LoginToken, client.OrdersFilter{Market: env.Market.Ref, ShowOpen: true})
	if err != nil {
		t.Fatalf("list open orders: %v", err)
	}
	if !containsOrder(open, orderID) {
		t.Fatalf("order %s missing from ?show_open (%d listed)", orderID, len(open))
	}

	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price, qty)

	// The notional moved from available to blocked — it left the balance but was not spent.
	resting := diffAgainst(t, ctx, acc.LoginToken, before)
	if got := resting[env.Market.QuoteSymbol]; got.Balance != -int64(notional) || got.Blocked != int64(notional) {
		t.Fatalf("%s while resting: balance=%d blocked=%d, want -%d/+%d",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, notional, notional)
	}

	// --- cancelled ---
	if err := env.Client.CancelOrder(ctx, acc.LoginToken, orderID); err != nil {
		t.Fatalf("cancel order: %v", err)
	}

	cancelled := assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, orderID)
	remainder := assert.Cancelled(t, cancelled)
	if remainder.RemainingHave != notional || remainder.RemainingWant != qty {
		t.Fatalf("cancelled remainder have=%d want=%d, expected the untouched %d/%d",
			remainder.RemainingHave, remainder.RemainingWant, notional, qty)
	}

	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price)

	listed, err := env.Client.ListOrders(ctx, acc.LoginToken, client.OrdersFilter{Market: env.Market.Ref, ShowCancelled: true})
	if err != nil {
		t.Fatalf("list cancelled orders: %v", err)
	}
	if !containsOrder(listed, orderID) {
		t.Fatalf("order %s missing from ?show_cancelled (%d listed)", orderID, len(listed))
	}

	// Nothing traded, so the account is exactly where it started.
	final := diffAgainst(t, ctx, acc.LoginToken, before)
	if got := final[env.Market.QuoteSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after cancel: balance=%d blocked=%d, want 0/0 — the reservation was not fully released",
			env.Market.QuoteSymbol, got.Balance, got.Blocked)
	}
}
