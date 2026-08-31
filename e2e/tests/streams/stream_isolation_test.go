//go:build e2e

package streams

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// S6 — one account's private stream never carries another account's orders.
//
// Subscribes as a bystander, then has two other accounts trade with each other.
// Expect: the trade completes and both participants see their own events, while the
// bystander's feed reports nothing about either order — private events route by user id, so a
// leak here would expose one trader's activity to another.
func TestPrivateStreamNeverLeaksAnotherAccountsOrders(t *testing.T) {
	ctx := env.Context(t)
	bystander := env.NewFundedAccount(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	bystanderFeed, err := stream.ConnectUser(ctx, env.Cfg.APIURL, bystander.LoginToken)
	if err != nil {
		t.Fatalf("subscribe as the bystander: %v", err)
	}
	defer bystanderFeed.Close()

	takerFeed, err := stream.ConnectUser(ctx, env.Cfg.APIURL, taker.LoginToken)
	if err != nil {
		t.Fatalf("subscribe as the taker: %v", err)
	}
	defer takerFeed.Close()

	price, qty := env.Band(t), env.MinQty()
	makerID, takerID := env.Trade(t, ctx, maker, taker, price, qty)

	// Wait until the taker's own feed has reported the trade, so the bystander's feed has
	// demonstrably had the chance to carry it too.
	assert.StreamStatus(t, ctx, takerFeed, takerID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled)

	// Give the bystander something of its own to receive, and treat that as the marker:
	// anything routed to it before its own event would be a leak.
	ownID := env.Send(t, ctx, bystander,
		fixtures.LimitBuy(env.Market, price-env.Market.PriceQuantum, qty))

	for {
		ev, err := bystanderFeed.Next(ctx)
		if err != nil {
			t.Fatalf("the bystander's stream ended before its own order arrived: %v", err)
		}
		if ev.OrderID == makerID || ev.OrderID == takerID {
			t.Fatalf("the bystander received an event for another account's order %s (status %q)",
				ev.OrderID, ev.Status)
		}
		if ev.OrderID == ownID {
			break // its own order came through; nothing foreign preceded it
		}
	}
}
