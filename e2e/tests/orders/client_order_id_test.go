//go:build e2e

package orders

import (
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O17 — a client_order_id can only be used once per account.
//
// Places an order carrying an id, then replays that id.
// Expect: the replay is refused at entry with a readable per-item error and never reaches the
// matching engine — the id is unique per user in the database, and a violation discovered
// there would abort the matcher's whole batch and force a book rebuild.
func TestClientOrderIDCannotBeReused(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	clientOrderID := fixtures.ClientOrderID()

	first := send(t, ctx, acc,
		fixtures.LimitBuy(env.Market, price, qty, fixtures.WithClientOrderID(clientOrderID)))
	assert.EventuallyResting(t, ctx, env.Client, acc.LoginToken, first)

	// The id is queryable, so the order really did land under it.
	listed, err := env.Client.ListOrders(ctx, acc.LoginToken,
		client.OrdersFilter{ClientOrderID: clientOrderID, ShowOpen: true})
	if err != nil {
		t.Fatalf("look the order up by client_order_id: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != first {
		t.Fatalf("lookup by client_order_id returned %d rows, want just %s", len(listed), first)
	}

	// --- the replay ---
	results, err := env.Client.CreateOrders(ctx, acc.LoginToken, []client.NewOrder{
		fixtures.LimitBuy(env.Market, price, qty, fixtures.WithClientOrderID(clientOrderID)),
	})
	if err != nil {
		t.Fatalf("batch call itself failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results for one order", len(results))
	}
	if results[0].Error == nil {
		t.Fatalf("the replayed client_order_id was accepted as %v", results[0].OrderID)
	}
	if !strings.Contains(*results[0].Error, "client_order_id") {
		t.Fatalf("rejection reads %q; it should name client_order_id so a caller can act on it",
			*results[0].Error)
	}
	if results[0].OrderID != nil {
		t.Fatalf("a rejected replay still got id %v", *results[0].OrderID)
	}

	// Nothing new reached the book: the account still has exactly the one order.
	open, err := env.Client.ListOrders(ctx, acc.LoginToken,
		client.OrdersFilter{Market: env.Market.Ref, ShowOpen: true})
	if err != nil {
		t.Fatalf("list open orders: %v", err)
	}
	if len(open) != 1 || open[0].ID != first {
		t.Fatalf("account has %d open orders after the replay, want only %s", len(open), first)
	}
}

// O17 — the id is unique per account, not globally: two users may pick the same one.
func TestClientOrderIDIsScopedToTheAccount(t *testing.T) {
	ctx := env.Context(t)
	first := env.NewFundedAccount(t)
	second := env.NewFundedAccount(t)

	price, qty := band(t), minQty()
	shared := fixtures.ClientOrderID()

	order := fixtures.LimitBuy(env.Market, price, qty, fixtures.WithClientOrderID(shared))

	firstID := send(t, ctx, first, order)
	secondID := send(t, ctx, second, order)

	assert.EventuallyResting(t, ctx, env.Client, first.LoginToken, firstID)
	assert.EventuallyResting(t, ctx, env.Client, second.LoginToken, secondID)

	if firstID == secondID {
		t.Fatal("the two accounts were given the same order id")
	}
}

// O17 — an order without a client_order_id is unconstrained; the check must not treat an
// absent id as a value that can collide.
func TestOrdersWithoutClientOrderIDAreUnconstrained(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()

	for i := 0; i < 3; i++ {
		id := send(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty))
		assert.EventuallyResting(t, ctx, env.Client, acc.LoginToken, id)
	}
}
