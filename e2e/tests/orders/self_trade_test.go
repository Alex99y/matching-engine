//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O18 — an account crossing its own resting order trades with itself.
//
// Documents current behaviour: there is no self-trade prevention (it is still an open design
// decision — see TODO-internal.MD), so this pins what the engine does today.
// Expect: both legs fill against each other, the account pays both the maker and taker fee,
// and it therefore ends the round trip down by exactly those fees and nothing more.
func TestSelfTradeIsCurrentlyAllowed(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()*10

	before := snapshot(t, ctx, acc.LoginToken)

	sellID := rest(t, ctx, acc, fixtures.LimitSell(env.Market, price, qty))
	buyID := send(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty))

	assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, buyID)
	assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, sellID)

	sell := fetch(t, ctx, acc.LoginToken, sellID)
	buy := fetch(t, ctx, acc.LoginToken, buyID)
	assert.Traded(t, sell)
	assert.Traded(t, buy)

	if sell.Matches[0].ID != buy.Matches[0].ID {
		t.Fatalf("the two legs did not trade with each other (match %s vs %s)",
			sell.Matches[0].ID, buy.Matches[0].ID)
	}
	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price)

	// Both legs are the same account, so the only thing that actually left is the fees.
	moved := diffAgainst(t, ctx, acc.LoginToken, before)
	if got, want := moved[env.Market.BaseSymbol].Net(), -int64(buy.Matches[0].Fee); got != want {
		t.Fatalf("%s net movement = %d, want %d (the buy-side fee alone)",
			env.Market.BaseSymbol, got, want)
	}
	if got, want := moved[env.Market.QuoteSymbol].Net(), -int64(sell.Matches[0].Fee); got != want {
		t.Fatalf("%s net movement = %d, want %d (the sell-side fee alone)",
			env.Market.QuoteSymbol, got, want)
	}
	if moved[env.Market.BaseSymbol].Blocked != 0 || moved[env.Market.QuoteSymbol].Blocked != 0 {
		t.Fatalf("funds still blocked after both legs closed: base=%d quote=%d",
			moved[env.Market.BaseSymbol].Blocked, moved[env.Market.QuoteSymbol].Blocked)
	}
}
