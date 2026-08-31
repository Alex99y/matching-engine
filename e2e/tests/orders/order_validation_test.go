//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O11 + O19 — orders that break the contract are refused before they reach the engine.
//
// Sends combinations the API is documented to reject: post-only on anything that cannot rest,
// market orders carrying the wrong denomination, and prices/quantities off the market's grid.
// Expect: each comes back as a per-item rejection with a message, never a queued order id, so
// nothing invalid ever reaches the matching engine.
func TestInvalidOrdersAreRejectedAtEntry(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	budget := env.Market.Notional(price, qty)

	cases := []struct {
		name  string
		order client.NewOrder
	}{
		// O11 — post-only is a resting-only modifier: limit + GTC or nothing.
		{"post-only on an IOC order",
			fixtures.LimitBuy(env.Market, price, qty, fixtures.WithTIF(client.IOC), fixtures.PostOnly())},
		{"post-only on a FOK order",
			fixtures.LimitBuy(env.Market, price, qty, fixtures.WithTIF(client.FOK), fixtures.PostOnly())},
		{"post-only on a market order",
			fixtures.MarketBuy(env.Market, budget, fixtures.PostOnly())},

		// A market order has no price to rest at, so it can never be GTC.
		{"market order with GTC", fixtures.MarketSell(env.Market, qty, fixtures.WithTIF(client.GTC))},

		// Market orders are denominated by side: a buy spends quote, a sell offers base.
		{"market buy carrying a base quantity",
			client.NewOrder{Market: env.Market.Ref, Side: client.Buy, Type: client.Market,
				TimeInForce: client.IOC, Quantity: qty}},
		{"market sell carrying a quote budget",
			client.NewOrder{Market: env.Market.Ref, Side: client.Sell, Type: client.Market,
				TimeInForce: client.IOC, QuoteQty: &budget}},

		// O19 — limit orders need a real price, and must sit on the market's grid.
		{"limit order without a price",
			client.NewOrder{Market: env.Market.Ref, Side: client.Buy, Type: client.Limit,
				TimeInForce: client.GTC, Quantity: qty}},
		{"quantity below the market minimum",
			fixtures.LimitBuy(env.Market, price, env.Market.MinOrderSize-1)},

		{"unknown market",
			client.NewOrder{Market: "NOPE-NOPE", Side: client.Buy, Type: client.Limit,
				TimeInForce: client.GTC, Price: price, Quantity: qty}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := env.Client.CreateOrders(ctx, acc.LoginToken, []client.NewOrder{tc.order})
			if err != nil {
				t.Fatalf("batch call itself failed: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("got %d results for one order", len(results))
			}
			if results[0].Error == nil {
				t.Fatalf("order was accepted as %v, want a rejection", results[0].OrderID)
			}
			if results[0].OrderID != nil {
				t.Fatalf("a rejected order still got id %v", *results[0].OrderID)
			}
		})
	}
}

// O19 — a price off the tick grid is refused, on markets that have a grid to be off.
func TestPriceOffTheTickGridIsRejected(t *testing.T) {
	if env.Market.PriceQuantum <= 1 {
		t.Skipf("market %s has no tick grid to violate (price_quantum=%d)",
			env.Market.Ref, env.Market.PriceQuantum)
	}

	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	offGrid := band(t) + 1 // guaranteed not a multiple of a quantum > 1
	results, err := env.Client.CreateOrders(ctx, acc.LoginToken,
		[]client.NewOrder{fixtures.LimitBuy(env.Market, offGrid, minQty())})
	if err != nil {
		t.Fatalf("batch call itself failed: %v", err)
	}
	if results[0].Error == nil {
		t.Fatalf("price %d off the %d tick grid was accepted", offGrid, env.Market.PriceQuantum)
	}
}
