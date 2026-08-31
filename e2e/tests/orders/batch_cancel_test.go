//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/google/uuid"
)

// O14 — several resting orders can be cancelled in one call.
//
// Rests three orders at neighbouring prices and cancels them together, alongside an id the
// account does not own.
// Expect: the three leave the book and release their reservations, the unknown id comes back
// as a per-item error rather than failing the whole call, and results stay aligned with the
// ids that were sent.
func TestBatchCancelClearsSeveralOrders(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	base, qty := band(t), minQty()
	prices := []uint64{base, base - env.Market.PriceQuantum, base - 2*env.Market.PriceQuantum}

	before := snapshot(t, ctx, acc.LoginToken)

	ids := make([]string, 0, len(prices)+1)
	for _, price := range prices {
		ids = append(ids, rest(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty)))
	}
	stranger := uuid.NewString()
	ids = append(ids, stranger)

	results, err := env.Client.CancelOrders(ctx, acc.LoginToken, ids)
	if err != nil {
		t.Fatalf("batch cancel: %v", err)
	}
	if len(results) != len(ids) {
		t.Fatalf("got %d results for %d ids", len(results), len(ids))
	}
	for i, r := range results {
		if r.OrderID != ids[i] {
			t.Fatalf("result %d is for %s, want %s — results must stay aligned", i, r.OrderID, ids[i])
		}
	}
	for i := range prices {
		if results[i].Error != nil {
			t.Fatalf("cancelling own order %s failed: %s", ids[i], *results[i].Error)
		}
	}
	if results[len(prices)].Error == nil {
		t.Fatalf("cancelling an id the account does not own (%s) reported success", stranger)
	}

	for i, price := range prices {
		assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, ids[i])
		assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price)
	}

	// Nothing traded, so every reservation is back.
	if got := diffAgainst(t, ctx, acc.LoginToken, before)[env.Market.QuoteSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after cancelling everything: balance=%d blocked=%d, want 0/0",
			env.Market.QuoteSymbol, got.Balance, got.Blocked)
	}
}
