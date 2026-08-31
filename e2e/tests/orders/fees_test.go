//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O16 — fees are taken out of what each side receives, at the taker's expense.
//
// Crosses one maker with one taker and inspects both sides of the same match. The API does
// not publish a market's fee rates, so this asserts the shape rather than the numbers.
// Expect: each party's fee is charged in the asset it received (base for the buyer, quote for
// the seller), the credit is the fill minus that fee, and the taker's effective rate is no
// better than the maker's.
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

	// The buyer received base; the seller received quote. Each is credited the fill minus
	// the fee charged in that same asset.
	if got, want := takerMoved[env.Market.BaseSymbol].Balance, int64(qty-takerFill.Fee); got != want {
		t.Fatalf("buyer credited %d %s, want %d (%d filled less a %d fee)",
			got, env.Market.BaseSymbol, want, qty, takerFill.Fee)
	}
	if got, want := makerMoved[env.Market.QuoteSymbol].Balance, int64(notional-makerFill.Fee); got != want {
		t.Fatalf("seller credited %d %s, want %d (%d notional less a %d fee)",
			got, env.Market.QuoteSymbol, want, notional, makerFill.Fee)
	}

	// A fee only ever reduces what you receive; it is never charged on top.
	if takerFill.Fee > qty {
		t.Fatalf("buyer's fee %d exceeds the %d base it bought", takerFill.Fee, qty)
	}
	if makerFill.Fee > notional {
		t.Fatalf("seller's fee %d exceeds the %d quote it earned", makerFill.Fee, notional)
	}

	// Compare the two rates without knowing either denomination:
	//   takerFee/qty >= makerFee/notional  ⟺  takerFee*notional >= makerFee*qty
	if takerFill.Fee*notional < makerFill.Fee*qty {
		t.Fatalf("the taker was charged a better rate than the maker (taker %d/%d vs maker %d/%d)",
			takerFill.Fee, qty, makerFill.Fee, notional)
	}

	assert.Conserved(t, env.Market.BaseSymbol, env.Market.QuoteSymbol,
		[]map[string]assert.Move{makerMoved, takerMoved}, makerOrder, takerOrder)
}
