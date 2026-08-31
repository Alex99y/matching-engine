//go:build e2e

package orders

import (
	"math/bits"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O16 — fees are charged at the market's published rates, on what each side receives.
//
// Crosses one maker with one taker and checks both sides of the same match against the
// taker_fee_bps / maker_fee_bps the market advertises.
// Expect: the buyer pays its rate in base and the seller pays its rate in quote, each fee is
// the received amount times the rate in basis points (floored), and each party is credited
// the fill minus exactly that.
func TestFeesAreChargedOnWhatEachSideReceives(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	// Enough size that basis-point fees do not floor away to nothing.
	price := band(t)
	qty := minQty() * 10
	notional := env.Market.Notional(price, qty)

	makerBefore := snapshot(t, ctx, maker.LoginToken)
	takerBefore := snapshot(t, ctx, taker.LoginToken)

	makerID := rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, qty))
	takerID := send(t, ctx, taker, fixtures.LimitBuy(env.Market, price, qty))

	assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, takerID)
	assert.EventuallyNotResting(t, ctx, env.Client, maker.LoginToken, makerID)

	makerOrder := fetch(t, ctx, maker.LoginToken, makerID)
	takerOrder := fetch(t, ctx, taker.LoginToken, takerID)
	assert.Traded(t, makerOrder)
	assert.Traded(t, takerOrder)

	makerFill, takerFill := makerOrder.Matches[0], takerOrder.Matches[0]
	if makerFill.IsTaker || !takerFill.IsTaker {
		t.Fatalf("taker/maker roles are backwards: resting isTaker=%v, crossing isTaker=%v",
			makerFill.IsTaker, takerFill.IsTaker)
	}

	makerMoved := diffAgainst(t, ctx, maker.LoginToken, makerBefore)
	takerMoved := diffAgainst(t, ctx, taker.LoginToken, takerBefore)

	// The buyer received base and the seller quote, each taxed at its own published rate.
	if want := feeOf(qty, env.Market.TakerFeeBps); takerFill.Fee != want {
		t.Fatalf("buyer's fee = %d, want %d (%d base at %d bps)",
			takerFill.Fee, want, qty, env.Market.TakerFeeBps)
	}
	if want := feeOf(notional, env.Market.MakerFeeBps); makerFill.Fee != want {
		t.Fatalf("seller's fee = %d, want %d (%d quote at %d bps)",
			makerFill.Fee, want, notional, env.Market.MakerFeeBps)
	}

	if got, want := takerMoved[env.Market.BaseSymbol].Balance, int64(qty-takerFill.Fee); got != want {
		t.Fatalf("buyer credited %d %s, want %d (%d filled less a %d fee)",
			got, env.Market.BaseSymbol, want, qty, takerFill.Fee)
	}
	if got, want := makerMoved[env.Market.QuoteSymbol].Balance, int64(notional-makerFill.Fee); got != want {
		t.Fatalf("seller credited %d %s, want %d (%d notional less a %d fee)",
			got, env.Market.QuoteSymbol, want, notional, makerFill.Fee)
	}

	assert.Conserved(t, env.Market.BaseSymbol, env.Market.QuoteSymbol,
		[]map[string]assert.Move{makerMoved, takerMoved}, makerOrder, takerOrder)
}

// feeOf mirrors the engine's own rounding: amount x bps / 10000, floored, through a 128-bit
// intermediate so a large quote notional cannot overflow the multiply.
func feeOf(amount, bps uint64) uint64 {
	if bps == 0 {
		return 0
	}
	hi, lo := bits.Mul64(amount, bps)
	fee, _ := bits.Div64(hi, lo, 10000)
	return fee
}
