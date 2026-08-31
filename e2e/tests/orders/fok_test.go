//go:build e2e

package orders

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// O5 — fill-or-kill is all or nothing.
//
// Against a book holding one lot, sends a FOK buy for three; then a FOK buy for exactly one.
// Expect: the oversized order is killed with no fills at all — the resting lot is left
// untouched — while the one that the book can satisfy fills completely.
func TestFillOrKillIsAllOrNothing(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price := band(t)
	available := minQty()

	makerID := rest(t, ctx, maker, fixtures.LimitSell(env.Market, price, available))
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price, available)

	// --- more than the book can fill: killed, and the book is untouched ---
	before := snapshot(t, ctx, taker.LoginToken)
	tooBig := send(t, ctx, taker,
		fixtures.LimitBuy(env.Market, price, available*3, fixtures.WithTIF(client.FOK)))

	killed := assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, tooBig)
	if len(killed.Matches) != 0 {
		t.Fatalf("a FOK order the book could not fill traded anyway: %+v", killed.Matches)
	}
	assert.Cancelled(t, killed)

	if got := diffAgainst(t, ctx, taker.LoginToken, before); got[env.Market.QuoteSymbol].Net() != 0 {
		t.Fatalf("a killed FOK moved %d %s, want 0", got[env.Market.QuoteSymbol].Net(), env.Market.QuoteSymbol)
	}
	// The maker never gave anything up.
	assert.EventuallyLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price, available)
	assert.Resting(t, fetch(t, ctx, maker.LoginToken, makerID))

	// --- exactly what the book can fill: trades in full ---
	exact := send(t, ctx, taker,
		fixtures.LimitBuy(env.Market, price, available, fixtures.WithTIF(client.FOK)))

	filled := assert.EventuallyNotResting(t, ctx, env.Client, taker.LoginToken, exact)
	assert.Traded(t, filled)
	if got := filled.Matches[0].BaseAmount; got != available {
		t.Fatalf("FOK filled %d base, want the full %d", got, available)
	}
	if filled.CancelledOrder != nil {
		t.Fatalf("a fully filled FOK recorded a cancelled remainder: %+v", *filled.CancelledOrder)
	}
	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Sell, price)
}
