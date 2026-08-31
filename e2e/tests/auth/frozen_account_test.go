//go:build e2e

package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// A7 — a frozen account can unwind but cannot take on new risk.
//
// Rests an order, freezes the account (via the cli — the API has no freeze route), then
// exercises the routes on either side of RequireNotFrozen.
// Expect: while frozen, POST /orders and POST /faucet return 403 while reads and cancelling
// the resting order still work; unfreezing restores placement. The session is never revoked —
// the frozen flag is read from the users table on every request.
func TestFrozenAccountCanCancelButNotTrade(t *testing.T) {
	ctx := env.Context(t)
	admin := env.Admin(t) // skips the test when the cli binary is unavailable
	acc := env.NewFundedAccount(t)

	order := fixtures.LimitBuy(env.Market,
		fixtures.RestingBidPrice(env.Market),
		fixtures.Qty(env.Market, fixtures.MinLots(env.Market)))

	orderID, err := env.Client.CreateOrder(ctx, acc.LoginToken, order)
	if err != nil {
		t.Fatalf("place the order to be cancelled later: %v", err)
	}
	assert.EventuallyResting(t, ctx, env.Client, acc.LoginToken, orderID)

	if err := admin.FreezeUser(ctx, acc.Username); err != nil {
		t.Fatalf("freeze %s: %v", acc.Username, err)
	}
	// Unfreeze on the way out too: the happy path below unfreezes explicitly, but an early
	// failure must not leave a frozen account behind for the cleanup sweep to trip over.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := admin.UnfreezeUser(cleanupCtx, acc.Username); err != nil {
			t.Logf("unfreeze %s: %v", acc.Username, err)
		}
	})

	// Fund-moving routes are closed...
	if _, err := env.Client.CreateOrders(ctx, acc.LoginToken, []client.NewOrder{order}); err == nil {
		t.Fatal("a frozen account placed an order, want 403")
	} else if got := client.Status(err); got != http.StatusForbidden {
		t.Fatalf("POST /orders while frozen: status = %d, want %d — %v", got, http.StatusForbidden, err)
	}
	if _, err := env.Client.RequestFunds(ctx, acc.LoginToken, env.Market.QuoteSymbol); err == nil {
		t.Fatal("a frozen account drew from the faucet, want 403")
	} else if got := client.Status(err); got != http.StatusForbidden {
		t.Fatalf("POST /faucet while frozen: status = %d, want %d — %v", got, http.StatusForbidden, err)
	}

	// ...but the session is still valid, and unwinding is still allowed.
	if _, err := env.Client.GetBalances(ctx, acc.LoginToken); err != nil {
		t.Fatalf("a frozen account cannot read its balances: %v", err)
	}
	if err := env.Client.CancelOrder(ctx, acc.LoginToken, orderID); err != nil {
		t.Fatalf("a frozen account cannot cancel its resting order: %v", err)
	}
	assert.EventuallyNotResting(t, ctx, env.Client, acc.LoginToken, orderID)

	if err := admin.UnfreezeUser(ctx, acc.Username); err != nil {
		t.Fatalf("unfreeze %s: %v", acc.Username, err)
	}
	if _, err := env.Client.CreateOrder(ctx, acc.LoginToken, order); err != nil {
		t.Fatalf("placing an order after unfreeze: %v", err)
	}
}
