//go:build e2e

package orders

import (
	"testing"
	"time"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// O15 — a GTC order with an expiry is reaped once its time is up.
//
// Rests a buy carrying expires_at a couple of seconds out and then waits.
// Expect: it rests first, then leaves the book on its own with no cancel request; the private
// stream reports "expired" (distinct from a user cancel) and the reservation is released.
func TestOrderWithTTLIsReapedWhenItExpires(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	price, qty := band(t), minQty()

	events, err := stream.ConnectUser(ctx, env.Cfg.APIURL, acc.LoginToken)
	if err != nil {
		t.Fatalf("subscribe to the order stream: %v", err)
	}
	defer events.Close()

	before := snapshot(t, ctx, acc.LoginToken)

	// Far enough out that the order is observably resting first, close enough that the
	// one-second sweep reaps it well inside the test deadline.
	expiresAt := time.Now().Add(3 * time.Second).Unix()
	orderID := send(t, ctx, acc,
		fixtures.LimitBuy(env.Market, price, qty, fixtures.ExpiresAt(expiresAt)))

	resting := assert.EventuallyResting(t, ctx, env.Client, acc.LoginToken, orderID)
	if resting.ExpiresAt == nil {
		t.Fatal("the resting order carries no expires_at")
	}
	if *resting.ExpiresAt != expiresAt {
		t.Fatalf("expires_at = %d, want %d", *resting.ExpiresAt, expiresAt)
	}

	ev := assert.StreamStatus(t, ctx, events, orderID,
		stream.StatusExpired, stream.StatusCancelled, stream.StatusFilled)
	if ev.Status != stream.StatusExpired {
		t.Fatalf("the reaped order reported %q, want %q — a TTL reap must be distinguishable "+
			"from a user cancel", ev.Status, stream.StatusExpired)
	}

	assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, orderID)
	assert.EventuallyNoLevel(t, ctx, env.Client, env.Market.Ref, client.Buy, price)

	if got := diffAgainst(t, ctx, acc.LoginToken, before)[env.Market.QuoteSymbol]; got.Balance != 0 || got.Blocked != 0 {
		t.Fatalf("%s after the order expired: balance=%d blocked=%d, want 0/0",
			env.Market.QuoteSymbol, got.Balance, got.Blocked)
	}
}
