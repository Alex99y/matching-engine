//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O13 — a batch is graded per item, not all-or-nothing.
//
// Submits three orders in one call where the middle one is invalid.
// Expect: the two valid orders are queued with ids and come to rest, the invalid one comes
// back with an error and no id, and every result keeps the index of the order it belongs to.
func TestBatchCreateReportsPerItemResults(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	batch := []client.NewOrder{
		fixtures.LimitBuy(env.Market, price, qty),
		fixtures.LimitBuy(env.Market, 0, qty), // invalid: a limit order needs a price
		fixtures.LimitBuy(env.Market, price-env.Market.PriceQuantum, qty),
	}

	results, err := env.Client.CreateOrders(ctx, acc.LoginToken, batch)
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if len(results) != len(batch) {
		t.Fatalf("got %d results for %d orders", len(results), len(batch))
	}

	for i, r := range results {
		if r.Index != i {
			t.Fatalf("result %d carries index %d — results must stay aligned with the request", i, r.Index)
		}
	}
	if results[1].Error == nil {
		t.Fatalf("the invalid order at index 1 was accepted as %v", results[1].OrderID)
	}
	for _, i := range []int{0, 2} {
		if results[i].Error != nil {
			t.Fatalf("valid order at index %d was rejected: %s", i, *results[i].Error)
		}
		if results[i].OrderID == nil {
			t.Fatalf("valid order at index %d got no id", i)
		}
	}

	// The accepted ones really did reach the engine.
	for _, i := range []int{0, 2} {
		assert.EventuallyResting(t, ctx, env.Client, acc.LoginToken, *results[i].OrderID)
	}
}

// O13 — a batch where nothing is valid still answers per item.
func TestBatchCreateWithNothingValid(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	results, err := env.Client.CreateOrders(ctx, acc.LoginToken, []client.NewOrder{
		fixtures.LimitBuy(env.Market, 0, minQty()),
		{Market: "NOPE-NOPE", Side: client.Buy, Type: client.Limit, TimeInForce: client.GTC,
			Price: band(t), Quantity: minQty()},
	})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results for 2 orders", len(results))
	}
	for i, r := range results {
		if r.Error == nil {
			t.Fatalf("invalid order at index %d was accepted as %v", i, r.OrderID)
		}
	}
}
