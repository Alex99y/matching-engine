//go:build e2e

package auth

import (
	"net/http"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
)

// A4 — a read-scoped token can look but not trade.
//
// Mints a read-scoped token from a login session and uses it on both a read and a write
// route.
// Expect: GET /orders succeeds (the token is valid), POST /orders and POST /faucet are
// refused with 403 by RequireWrite, and nothing the read token sent reaches the book.
func TestReadScopedTokenCannotTrade(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	readToken, err := acc.ReadToken(ctx, env.Client)
	if err != nil {
		t.Fatalf("mint read-scoped token: %v", err)
	}

	// Reading is allowed — proves the token authenticates and the 403s below are about
	// scope, not a rejected token.
	if _, err := env.Client.ListOrders(ctx, readToken, client.OrdersFilter{Market: env.Market.Ref}); err != nil {
		t.Fatalf("GET /orders with a read token: %v", err)
	}

	order := fixtures.LimitBuy(env.Market,
		fixtures.RestingBidPrice(env.Market),
		fixtures.Qty(env.Market, fixtures.MinLots(env.Market)))

	if _, err := env.Client.CreateOrders(ctx, readToken, []client.NewOrder{order}); err == nil {
		t.Fatal("POST /orders with a read token succeeded, want 403")
	} else if got := client.Status(err); got != http.StatusForbidden {
		t.Fatalf("POST /orders status = %d, want %d — %v", got, http.StatusForbidden, err)
	}

	if _, err := env.Client.RequestFunds(ctx, readToken, env.Market.QuoteSymbol); err == nil {
		t.Fatal("POST /faucet with a read token succeeded, want 403")
	} else if got := client.Status(err); got != http.StatusForbidden {
		t.Fatalf("POST /faucet status = %d, want %d — %v", got, http.StatusForbidden, err)
	}

	// The refused order must not have reached the matching engine.
	open, err := env.Client.ListOrders(ctx, acc.LoginToken, client.OrdersFilter{
		Market: env.Market.Ref, ShowOpen: true,
	})
	if err != nil {
		t.Fatalf("list the account's orders: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("account has %d order(s) after a refused write, want 0", len(open))
	}

	// The account's own login token is write-scoped, so the same order is accepted from it.
	if _, err := env.Client.CreateOrder(ctx, acc.LoginToken, order); err != nil {
		t.Fatalf("the same order from the login token: %v", err)
	}
}
