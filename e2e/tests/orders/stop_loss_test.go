//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// O22 — a stop loss on a filled buy fires once the market trades down to the trigger.
//
// Fills a bracket buy, then prints trades on the way down with two other accounts while a bid
// waits one tick under the stop.
// Expect: a trade one tick above the stop leaves the exit parked; a trade at the stop fires
// it into the waiting bid — the whole position sold, its base gone from blocked, the quote
// proceeds (less the taker fee) spendable, the exit read back filled. Losing money is the
// point: the sale price is below what the entry paid.
func TestStopLossFiresWhenPriceFallsToTrigger(t *testing.T) {
	ctx := env.Context(t)
	trader := env.NewFundedAccount(t)
	cp := env.NewFundedAccount(t)
	mover := env.NewFundedAccount(t)

	tick := env.Market.PriceQuantum
	price := band(t)
	takeProfit, stopLoss := price+4*tick, price-4*tick
	exitBid := stopLoss - tick
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
		fixtures.LimitBuy(env.Market, price, qty, fixtures.TakeProfit(takeProfit), fixtures.StopLoss(stopLoss)))
	if err != nil {
		t.Fatalf("place bracket buy: %v", err)
	}
	entry := assert.EventuallyNotResting(t, ctx, env.Client, trader.LoginToken, entryID)
	assert.Traded(t, entry)
	received := entry.Matches[0].BaseAmount - entry.Matches[0].Fee
	assert.StreamStatus(t, ctx, events, exitID, stream.StatusPending)

	// --- one tick above the stop: nothing happens ---
	env.Trade(t, ctx, mover, cp, stopLoss+tick, minQty())
	assert.Pending(t, fetch(t, ctx, trader.LoginToken, exitID))

	// --- at the stop: the exit fires into the bid waiting under it ---
	rest(t, ctx, cp, fixtures.LimitBuy(env.Market, exitBid, qty))
	env.Trade(t, ctx, mover, cp, stopLoss, minQty())

	if ev := assert.StreamStatus(t, ctx, events, exitID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled); ev.Status != stream.StatusFilled {
		t.Fatalf("fired exit reported %q, want %q", ev.Status, stream.StatusFilled)
	}
	exit := assert.EventuallyStatus(t, ctx, env.Client, trader.LoginToken, exitID, client.StatusFilled)
	sold, proceeds, fee := exitFills(t, exit, exitBid)
	if sold != received {
		t.Fatalf("exit sold %d, want the whole %d it held", sold, received)
	}
	if proceeds >= notional {
		t.Fatalf("stop loss sold for %d, want less than the %d the entry paid", proceeds, notional)
	}

	closed := diffAgainst(t, ctx, trader.LoginToken, before)
	if got := closed[env.Market.BaseSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after the stop fired: balance=%d blocked=%d, want 0/0",
			env.Market.BaseSymbol, got.Balance, got.Blocked)
	}
	wantQuote := -int64(notional) + int64(proceeds-fee)
	if got := closed[env.Market.QuoteSymbol]; got.Balance != wantQuote || got.Blocked != 0 {
		t.Fatalf("%s after the stop fired: balance=%d blocked=%d, want %d/0 (-%d paid, +%d received less a %d fee)",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, wantQuote, notional, proceeds, fee)
	}
}

// O23 — the mirror image: a stop loss on a filled sell is a buy exit that fires on the way up,
// spending the quote the sale brought in.
//
// Fills a bracket sell against a resting bid, rests an ask one tick above the stop, then
// prints a trade at the stop.
// Expect: the exit is a quote-denominated market buy whose budget is exactly the quote the
// sell received net of its fee, blocked while parked; at the stop it fires into the ask, the
// budget leaves blocked, and the base bought (less the taker fee) lands in balance.
func TestStopLossOnSellEntryBuysBackWithItsQuoteProceeds(t *testing.T) {
	ctx := env.Context(t)
	trader := env.NewFundedAccount(t)
	cp := env.NewFundedAccount(t)
	mover := env.NewFundedAccount(t)

	tick := env.Market.PriceQuantum
	price := band(t)
	takeProfit, stopLoss := price-4*tick, price+4*tick // sell entry: profit below, stop above
	exitAsk := stopLoss + tick
	qty := minQty() * 10

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, trader.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, trader.LoginToken)
	rest(t, ctx, cp, fixtures.LimitBuy(env.Market, price, qty))

	entryID, exitID, err := env.Client.CreateBracketOrder(ctx, trader.LoginToken,
		fixtures.LimitSell(env.Market, price, qty, fixtures.TakeProfit(takeProfit), fixtures.StopLoss(stopLoss)))
	if err != nil {
		t.Fatalf("place bracket sell: %v", err)
	}
	entry := assert.EventuallyNotResting(t, ctx, env.Client, trader.LoginToken, entryID)
	assert.Traded(t, entry)
	entryFill := entry.Matches[0]
	received := entryFill.QuoteAmount - entryFill.Fee // a seller's fee is charged in quote
	assert.StreamStatus(t, ctx, events, exitID, stream.StatusPending)

	exit := fetch(t, ctx, trader.LoginToken, exitID)
	assert.Pending(t, exit)
	if exit.Side == nil || *exit.Side != string(client.Buy) || exit.Type != string(client.Market) {
		t.Fatalf("exit is a %v %s, want a market buy", exit.Side, exit.Type)
	}
	if exit.HaveQuantity != received {
		t.Fatalf("exit budget = %d %s, want the %d received (%d notional less a %d fee)",
			exit.HaveQuantity, env.Market.QuoteSymbol, received, entryFill.QuoteAmount, entryFill.Fee)
	}
	armed := diffAgainst(t, ctx, trader.LoginToken, before)
	if got := armed[env.Market.QuoteSymbol]; got.Balance != 0 || got.Blocked != int64(received) {
		t.Fatalf("%s after the entry filled: balance=%d blocked=%d, want 0/%d — the proceeds back the exit",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, received)
	}
	if got := armed[env.Market.BaseSymbol]; got.Balance != -int64(qty) || got.Blocked != 0 {
		t.Fatalf("%s after the entry filled: balance=%d blocked=%d, want -%d/0",
			env.Market.BaseSymbol, got.Balance, got.Blocked, qty)
	}

	// The ask the exit will lift has to be there before the trigger prints, or the exit would
	// fire into whatever else is on the book.
	rest(t, ctx, cp, fixtures.LimitSell(env.Market, exitAsk, qty))
	env.Trade(t, ctx, mover, cp, stopLoss, minQty())

	ev := assert.StreamStatus(t, ctx, events, exitID,
		stream.StatusFilled, stream.StatusPartiallyFilled, stream.StatusCancelled)
	if ev.Status == stream.StatusCancelled {
		t.Fatalf("fired exit was cancelled without buying anything")
	}
	exit = assert.EventuallyStatus(t, ctx, env.Client, trader.LoginToken, exitID,
		client.StatusFilled, client.StatusPartiallyFilled)
	bought, spent, fee := exitFills(t, exit, exitAsk)
	if spent > received {
		t.Fatalf("exit spent %d, more than the %d budget it held", spent, received)
	}

	// Budget out of blocked (spent, or refunded where it could not buy a unit), base in less
	// the taker's fee.
	closed := diffAgainst(t, ctx, trader.LoginToken, before)
	if got := closed[env.Market.QuoteSymbol]; got.Blocked != 0 || got.Balance != int64(received-spent) {
		t.Fatalf("%s after the stop fired: balance=%d blocked=%d, want %d/0 (unspent budget back, nothing left blocked)",
			env.Market.QuoteSymbol, got.Balance, got.Blocked, received-spent)
	}
	wantBase := -int64(qty) + int64(bought-fee)
	if got := closed[env.Market.BaseSymbol]; got.Balance != wantBase || got.Blocked != 0 {
		t.Fatalf("%s after the stop fired: balance=%d blocked=%d, want %d/0 (-%d sold, +%d bought less a %d fee)",
			env.Market.BaseSymbol, got.Balance, got.Blocked, wantBase, qty, bought, fee)
	}
}
