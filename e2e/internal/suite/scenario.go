package suite

import (
	"context"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/harness"
)

// The order book is shared by every test in a run (and by anything else pointed at the same
// market), so each test claims its own price band. bandSalt randomises where this run's bands
// start, so a leftover order from an earlier run can never be mistaken for this run's
// liquidity; bandSeq hands out a fresh, widely spaced band to each test that asks.
var (
	bandSeq  atomic.Uint64
	bandSalt = uint64(rand.Intn(64))
)

// Band returns a price no other test in this run is using — bands are one percent apart, so
// one test's orders can never cross another's. The base is high enough that a minimum-size
// order still carries a notional worth asserting on (fees included).
func (e *Env) Band(t *testing.T) uint64 {
	t.Helper()

	base := fixtures.TradablePrice(e.Market)
	step := base / 100
	if step < e.Market.PriceQuantum {
		step = e.Market.PriceQuantum
	}
	step = (step / e.Market.PriceQuantum) * e.Market.PriceQuantum

	return base + (bandSalt+bandSeq.Add(1))*step
}

// MinQty is the market's smallest legal order quantity.
func (e *Env) MinQty() uint64 {
	return fixtures.Qty(e.Market, fixtures.MinLots(e.Market))
}

// Send places an order without waiting for any particular outcome, returning its id.
func (e *Env) Send(t *testing.T, ctx context.Context, acc *harness.Account, order client.NewOrder) string {
	t.Helper()

	id, err := e.Client.CreateOrder(ctx, acc.LoginToken, order)
	if err != nil {
		t.Fatalf("place %s %s order: %v", order.TimeInForce, order.Side, err)
	}
	return id
}

// Rest places an order and waits until the matching engine has it in the book. Use it for the
// maker leg a test needs in place before the taker arrives.
func (e *Env) Rest(t *testing.T, ctx context.Context, acc *harness.Account, order client.NewOrder) string {
	t.Helper()

	id := e.Send(t, ctx, acc, order)
	assert.EventuallyResting(t, ctx, e.Client, acc.LoginToken, id)
	return id
}

// Trade rests a sell and crosses it with a buy of the same size, returning both order ids
// once neither is in the book any more. The maker and taker must be different accounts unless
// the test is deliberately exercising a self-trade.
func (e *Env) Trade(t *testing.T, ctx context.Context, maker, taker *harness.Account, price, qty uint64) (makerID, takerID string) {
	t.Helper()

	makerID = e.Rest(t, ctx, maker, fixtures.LimitSell(e.Market, price, qty))
	takerID = e.Send(t, ctx, taker, fixtures.LimitBuy(e.Market, price, qty))

	assert.EventuallyNotResting(t, ctx, e.Client, taker.LoginToken, takerID)
	assert.EventuallyNotResting(t, ctx, e.Client, maker.LoginToken, makerID)
	return makerID, takerID
}

// Fetch reads one order back, including its fills.
func (e *Env) Fetch(t *testing.T, ctx context.Context, token, orderID string) client.Order {
	t.Helper()

	o, err := e.Client.GetOrder(ctx, token, orderID)
	if err != nil {
		t.Fatalf("get order %s: %v", orderID, err)
	}
	return o
}

// Snapshot reads an account's balances, failing the test on error.
func (e *Env) Snapshot(t *testing.T, ctx context.Context, token string) assert.Balances {
	t.Helper()

	b, err := assert.Snapshot(ctx, e.Client, token)
	if err != nil {
		t.Fatalf("balance snapshot: %v", err)
	}
	return b
}

// DiffAgainst re-reads an account's balances and returns the movement since before.
func (e *Env) DiffAgainst(t *testing.T, ctx context.Context, token string, before assert.Balances) map[string]assert.Move {
	t.Helper()
	return assert.Diff(before, e.Snapshot(t, ctx, token))
}
