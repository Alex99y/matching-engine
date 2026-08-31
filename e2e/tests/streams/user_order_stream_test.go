//go:build e2e

package streams

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// S1 — the private stream narrates an order's whole life.
//
// Subscribes, rests a sell, then crosses it and watches both accounts' feeds.
// Expect: the maker sees "open" and then a terminal "filled" with the full quantity; the
// taker — which never rests — goes straight to "filled". Filled and remaining are consistent
// at every step.
func TestOrderStreamReportsLifecycleToTerminalStatus(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	makerFeed, err := stream.ConnectUser(ctx, env.Cfg.APIURL, maker.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the maker's stream: %v", err)
	}
	defer makerFeed.Close()

	takerFeed, err := stream.ConnectUser(ctx, env.Cfg.APIURL, taker.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the taker's stream: %v", err)
	}
	defer takerFeed.Close()

	price, qty := env.Band(t), env.MinQty()

	makerID := env.Send(t, ctx, maker, fixtures.LimitSell(env.Market, price, qty))
	open := assert.StreamStatus(t, ctx, makerFeed, makerID, stream.StatusOpen)
	if open.Filled != 0 {
		t.Fatalf("a freshly rested order reports %d filled, want 0", open.Filled)
	}
	if open.Remaining != qty {
		t.Fatalf("a freshly rested order reports %d remaining, want %d", open.Remaining, qty)
	}

	takerID := env.Send(t, ctx, taker, fixtures.LimitBuy(env.Market, price, qty))

	// The crossing order never rests, so its first and only event is terminal.
	takerDone := assert.StreamStatus(t, ctx, takerFeed, takerID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled, stream.StatusRejected)
	if takerDone.Status != stream.StatusFilled {
		t.Fatalf("taker finished as %q, want %q", takerDone.Status, stream.StatusFilled)
	}
	if takerDone.Filled != qty {
		t.Fatalf("taker reports %d filled, want %d", takerDone.Filled, qty)
	}

	makerDone := assert.StreamStatus(t, ctx, makerFeed, makerID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled)
	if makerDone.Status != stream.StatusFilled {
		t.Fatalf("maker finished as %q, want %q", makerDone.Status, stream.StatusFilled)
	}
	if makerDone.Filled != qty || makerDone.Remaining != 0 {
		t.Fatalf("a filled maker reports filled=%d remaining=%d, want %d/0",
			makerDone.Filled, makerDone.Remaining, qty)
	}
}

// S1 — a partial fill is reported as such, before the order finally leaves the book.
//
// Rests three lots and takes one, then cancels the rest.
// Expect: the maker's feed shows open → partially_filled with a third filled and two thirds
// remaining, then a terminal event once it is cancelled.
func TestOrderStreamReportsPartialFillsAsTheyHappen(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	feed, err := stream.ConnectUser(ctx, env.Cfg.APIURL, maker.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the maker's stream: %v", err)
	}
	defer feed.Close()

	price := env.Band(t)
	makerQty, takerQty := env.MinQty()*3, env.MinQty()

	makerID := env.Send(t, ctx, maker, fixtures.LimitSell(env.Market, price, makerQty))
	assert.StreamStatus(t, ctx, feed, makerID, stream.StatusOpen)

	env.Send(t, ctx, taker, fixtures.LimitBuy(env.Market, price, takerQty))

	partial := assert.StreamStatus(t, ctx, feed, makerID, stream.StatusPartiallyFilled)
	if partial.Filled != takerQty {
		t.Fatalf("partial fill reports %d filled, want %d", partial.Filled, takerQty)
	}
	if partial.Remaining != makerQty-takerQty {
		t.Fatalf("partial fill reports %d remaining, want %d", partial.Remaining, makerQty-takerQty)
	}

	if err := env.Client.CancelOrder(ctx, maker.LoginToken, makerID); err != nil {
		t.Fatalf("cancel the remainder: %v", err)
	}
	final := assert.StreamStatus(t, ctx, feed, makerID,
		stream.StatusCancelled, stream.StatusPartiallyFilled, stream.StatusFilled)
	if final.Filled != takerQty {
		t.Fatalf("after cancelling the remainder the order reports %d filled, want %d",
			final.Filled, takerQty)
	}
}
