//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// O20 — a take profit on a filled buy arms a sell exit that fires once the market trades at
// the trigger, and nowhere below it.
//
// Fills a bracket buy against a resting ask, then prints trades around the take-profit price
// with two other accounts while a bid waits just under it.
// Expect: the exit is announced at entry and shows up pending — parented to the entry, sized
// to what the buy received net of its fee, that amount blocked rather than spendable, listed
// with the open orders. A trade one tick under the trigger leaves it parked; a trade at the
// trigger fires it into the waiting bid, after which the base has left blocked, the quote
// proceeds (less the taker fee) are spendable, and the exit reads back filled.
func TestTakeProfitFiresWhenPriceReachesTrigger(t *testing.T) {
	ctx := env.Context(t)
	trader := env.NewFundedAccount(t)
	cp := env.NewFundedAccount(t)    // the trader's counterparty on both legs
	mover := env.NewFundedAccount(t) // prints the trigger price together with cp

	tick := env.Market.PriceQuantum
	price := band(t)
	takeProfit, stopLoss := price+4*tick, price-4*tick
	exitBid := takeProfit - tick
	qty := minQty() * 10 // large enough that the fee does not floor away
	notional := env.Market.Notional(price, qty)

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, trader.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, trader.LoginToken)
	rest(t, ctx, cp, fixtures.LimitSell(env.Market, price, qty))

	entryID, exitID, err := env.Client.CreateBracketOrder(ctx, trader.LoginToken,
		fixtures.LimitBuy(env.Market, price, qty, fixtures.TakeProfit(takeProfit), fixtures.StopLoss(stopLoss)))
	if err != nil {
		t.Fatalf("place bracket buy: %v", err)
	}

	entry := assert.EventuallyNotResting(t, ctx, env.Client, trader.LoginToken, entryID)
	assert.Traded(t, entry)
	entryFill := entry.Matches[0]
	received := entryFill.BaseAmount - entryFill.Fee

	// --- armed: the exit exists, pending, and the proceeds are blocked for it ---
	if ev := assert.StreamStatus(t, ctx, events, exitID, stream.StatusPending); ev.Remaining != received {
		t.Fatalf("pending exit announced with remaining %d, want the %d received", ev.Remaining, received)
	}
	exit := fetch(t, ctx, trader.LoginToken, exitID)
	assert.Pending(t, exit)
	if exit.ParentOrderID == nil || *exit.ParentOrderID != entryID {
		t.Fatalf("exit parent = %v, want the entry %s", exit.ParentOrderID, entryID)
	}
	if exit.Side == nil || *exit.Side != string(client.Sell) || exit.Type != string(client.Market) {
		t.Fatalf("exit is a %v %s, want a market sell", exit.Side, exit.Type)
	}
	if exit.HaveQuantity != received {
		t.Fatalf("exit offers %d %s, want the %d received (%d filled less a %d fee)",
			exit.HaveQuantity, env.Market.BaseSymbol, received, entryFill.BaseAmount, entryFill.Fee)
	}
	if exit.TakeProfitPrice == nil || *exit.TakeProfitPrice != takeProfit || exit.StopLossPrice == nil || *exit.StopLossPrice != stopLoss {
		t.Fatalf("exit triggers = (%v, %v), want (%d, %d)", exit.TakeProfitPrice, exit.StopLossPrice, takeProfit, stopLoss)
	}

	armed := diffAgainst(t, ctx, trader.LoginToken, before)
	if got := armed[env.Market.BaseSymbol]; got.Balance != 0 || got.Blocked != int64(received) {
		t.Fatalf("%s after the entry filled: balance=%d blocked=%d, want 0/%d — the proceeds back the exit",
			env.Market.BaseSymbol, got.Balance, got.Blocked, received)
	}
	if got := armed[env.Market.QuoteSymbol]; got.Balance != -int64(notional) || got.Blocked != 0 {
		t.Fatalf("%s after the entry filled: balance=%d blocked=%d, want -%d/0",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, notional)
	}

	open, err := env.Client.ListOrders(ctx, trader.LoginToken, client.OrdersFilter{Market: env.Market.Ref, ShowOpen: true})
	if err != nil {
		t.Fatalf("list open orders: %v", err)
	}
	if !containsOrder(open, exitID) {
		t.Fatalf("pending exit %s missing from the open-orders listing", exitID)
	}

	// --- one tick short: nothing happens ---
	env.Trade(t, ctx, mover, cp, takeProfit-tick, minQty())
	assert.Pending(t, fetch(t, ctx, trader.LoginToken, exitID))

	// --- at the trigger: the exit fires into the bid waiting under it ---
	rest(t, ctx, cp, fixtures.LimitBuy(env.Market, exitBid, qty))
	env.Trade(t, ctx, mover, cp, takeProfit, minQty())

	if ev := assert.StreamStatus(t, ctx, events, exitID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled); ev.Status != stream.StatusFilled {
		t.Fatalf("fired exit reported %q, want %q", ev.Status, stream.StatusFilled)
	}
	exit = assert.EventuallyStatus(t, ctx, env.Client, trader.LoginToken, exitID, client.StatusFilled)
	sold, proceeds, fee := exitFills(t, exit, exitBid)
	if sold != received {
		t.Fatalf("exit sold %d, want the whole %d it held", sold, received)
	}

	closed := diffAgainst(t, ctx, trader.LoginToken, before)
	if got := closed[env.Market.BaseSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after the exit fired: balance=%d blocked=%d, want 0/0 — the position left through blocked",
			env.Market.BaseSymbol, got.Balance, got.Blocked)
	}
	wantQuote := -int64(notional) + int64(proceeds-fee)
	if got := closed[env.Market.QuoteSymbol]; got.Balance != wantQuote || got.Blocked != 0 {
		t.Fatalf("%s after the exit fired: balance=%d blocked=%d, want %d/0 (-%d paid, +%d received less a %d fee)",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, wantQuote, notional, proceeds, fee)
	}
}

// O21 — a pending exit is a live order: its owner can cancel it, which frees the position.
//
// Fills a bracket buy, then cancels the announced exit before any trigger is reached.
// Expect: the cancel is accepted through the ordinary route, the exit reads back cancelled for
// its full size with nothing traded, the blocked base is spendable again, and a later trade at
// the take-profit price no longer fires anything.
func TestPendingExitCanBeCancelledAndReleasesItsFunds(t *testing.T) {
	ctx := env.Context(t)
	trader := env.NewFundedAccount(t)
	cp := env.NewFundedAccount(t)
	mover := env.NewFundedAccount(t)

	tick := env.Market.PriceQuantum
	price := band(t)
	takeProfit := price + 4*tick
	qty := minQty() * 10
	notional := env.Market.Notional(price, qty)

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, trader.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, trader.LoginToken)
	rest(t, ctx, cp, fixtures.LimitSell(env.Market, price, qty))

	entryID, exitID, err := env.Client.CreateBracketOrder(ctx, trader.LoginToken,
		fixtures.LimitBuy(env.Market, price, qty, fixtures.TakeProfit(takeProfit)))
	if err != nil {
		t.Fatalf("place bracket buy: %v", err)
	}
	entry := assert.EventuallyNotResting(t, ctx, env.Client, trader.LoginToken, entryID)
	assert.Traded(t, entry)
	received := entry.Matches[0].BaseAmount - entry.Matches[0].Fee
	assert.StreamStatus(t, ctx, events, exitID, stream.StatusPending)

	if err := env.Client.CancelOrder(ctx, trader.LoginToken, exitID); err != nil {
		t.Fatalf("cancel the pending exit: %v", err)
	}

	if ev := assert.StreamStatus(t, ctx, events, exitID,
		stream.StatusCancelled, stream.StatusFilled, stream.StatusPartiallyFilled); ev.Status != stream.StatusCancelled {
		t.Fatalf("cancelled exit reported %q, want %q", ev.Status, stream.StatusCancelled)
	}
	exit := assert.EventuallyStatus(t, ctx, env.Client, trader.LoginToken, exitID, client.StatusCancelled)
	if len(exit.Matches) != 0 {
		t.Fatalf("a cancelled exit traded: %+v", exit.Matches)
	}
	if remainder := assert.Cancelled(t, exit); remainder.RemainingHave != received {
		t.Fatalf("cancelled remainder have=%d, want the whole %d the exit held", remainder.RemainingHave, received)
	}

	// The position is the trader's to spend again; the quote paid for it is simply gone.
	moved := diffAgainst(t, ctx, trader.LoginToken, before)
	if got := moved[env.Market.BaseSymbol]; got.Balance != int64(received) || got.Blocked != 0 {
		t.Fatalf("%s after cancelling the exit: balance=%d blocked=%d, want %d/0",
			env.Market.BaseSymbol, got.Balance, got.Blocked, received)
	}
	if got := moved[env.Market.QuoteSymbol]; got.Balance != -int64(notional) || got.Blocked != 0 {
		t.Fatalf("%s after cancelling the exit: balance=%d blocked=%d, want -%d/0",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, notional)
	}

	// Reaching the trigger now fires nothing: the exit is gone, not merely disarmed.
	env.Trade(t, ctx, mover, cp, takeProfit, minQty())
	if after := fetch(t, ctx, trader.LoginToken, exitID); after.Status != client.StatusCancelled || len(after.Matches) != 0 {
		t.Fatalf("cancelled exit changed after the trigger traded: %q with %d fill(s)", after.Status, len(after.Matches))
	}
}
