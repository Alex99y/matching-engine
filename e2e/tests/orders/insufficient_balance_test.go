//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// O12 — an order the account cannot fund is rejected at reservation, asynchronously.
//
// Sends a buy far larger than an unfunded account's balance.
// Expect: the API still accepts it (the balance check happens in the engine, not at entry),
// but nothing rests, the private stream reports "rejected" — distinct from a user cancel —
// and no funds move.
func TestUnfundedOrderIsRejectedByTheEngine(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t) // deliberately never funded

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, acc.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, acc.LoginToken)

	price, qty := band(t), minQty()
	orderID, err := env.Client.CreateOrder(ctx, acc.LoginToken, fixtures.LimitBuy(env.Market, price, qty))
	if err != nil {
		t.Fatalf("the API refused the order at entry; the funds check belongs to the engine: %v", err)
	}

	ev := assert.StreamStatus(t, ctx, events, orderID,
		stream.StatusRejected, stream.StatusOpen, stream.StatusFilled, stream.StatusPartiallyFilled)
	if ev.Status != stream.StatusRejected {
		t.Fatalf("unfunded order reported %q, want %q", ev.Status, stream.StatusRejected)
	}
	if ev.Filled != 0 {
		t.Fatalf("a rejected order reports %d filled, want 0", ev.Filled)
	}

	order := assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, orderID)
	if len(order.Matches) != 0 {
		t.Fatalf("a rejected order traded: %+v", order.Matches)
	}
	assert.Cancelled(t, order)

	// An account with nothing in it cannot have moved anything.
	for symbol, moved := range diffAgainst(t, ctx, acc.LoginToken, before) {
		if moved.Balance != 0 || moved.Blocked != 0 {
			t.Fatalf("%s moved (balance=%d blocked=%d) on a rejected order, want 0/0",
				symbol, moved.Balance, moved.Blocked)
		}
	}
}
