//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O3 — a taker smaller than the resting maker fills itself and leaves the maker in the book.
//
// Rests a sell of three lots, then buys one lot at the same price.
// Expect: the taker is fully filled, the maker is still resting with two lots left, the book
// shows exactly that reduced quantity, and the maker's blocked base shrinks by what it sold.
func TestPartialFillLeavesMakerResting(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price := band(t)
	makerQty := minQty() * 3
	takerQty := minQty()
	remaining := makerQty - takerQty

	makerID := rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, makerQty))
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price, makerQty)

	// Snapshot once the maker is resting, so the movement below is the fill alone rather
	// than the fill plus the entry reservation.
	makerResting := snapshot(t, ctx, maker.LoginToken)

	takerID := send(t, ctx, taker, fixtures.LimitBuy(env.Market, price, takerQty))
	assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, takerID)

	// The taker got everything it asked for.
	takerOrder := fetch(t, ctx, taker.LoginToken, takerID)
	assert.Traded(t, takerOrder)
	if takerOrder.CancelledOrder != nil {
		t.Fatalf("a fully filled taker recorded a cancelled remainder: %+v", *takerOrder.CancelledOrder)
	}

	// The maker is still there, minus what it sold.
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price, remaining)

	makerOrder := fetch(t, ctx, maker.LoginToken, makerID)
	leg := assert.Resting(t, makerOrder)
	// A sell holds base and wants quote.
	if leg.RemainingHave != remaining {
		t.Fatalf("maker still holds %d base, want %d", leg.RemainingHave, remaining)
	}
	if want := env.Market.Notional(price, remaining); leg.RemainingWant != want {
		t.Fatalf("maker still wants %d quote, want %d", leg.RemainingWant, want)
	}
	assert.Traded(t, makerOrder)

	// Only the traded base left the maker's blocked pot; the remainder is still reserved.
	makerMoved := diffAgainst(t, ctx, maker.LoginToken, makerResting)
	if got := makerMoved[env.Market.BaseSymbol]; got.Blocked != -int64(takerQty) {
		t.Fatalf("maker's blocked %s moved %d, want -%d (only the traded part)",
			env.Market.BaseSymbol, got.Blocked, takerQty)
	}
	if got := makerMoved[env.Market.QuoteSymbol]; got.Balance <= 0 {
		t.Fatalf("maker was credited %d %s for the fill, want a positive amount",
			got.Balance, env.Market.QuoteSymbol)
	}
}
