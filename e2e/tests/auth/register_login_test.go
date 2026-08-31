//go:build e2e

package auth

import "testing"

// A1 — registration through to a usable session.
//
// Registers a new account, logs in, and spends the returned token on the faucet (a route
// behind Auth + RequireWrite + RequireNotFrozen).
// Expect: the token is accepted and the credited amount shows up in the account's balances.
func TestRegisterThenLoginGrantsWriteAccess(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t) // registers + logs in; fails the test if either step does

	if acc.LoginToken == "" {
		t.Fatal("login returned an empty token")
	}

	credit, err := env.Client.RequestFunds(ctx, acc.LoginToken, env.Market.QuoteSymbol)
	if err != nil {
		t.Fatalf("faucet with a fresh login token: %v", err)
	}
	if credit.Amount <= 0 {
		t.Fatalf("faucet credited %d %s, want a positive amount", credit.Amount, credit.Symbol)
	}

	balance, err := env.Client.Balance(ctx, acc.LoginToken, env.Market.QuoteSymbol)
	if err != nil {
		t.Fatalf("read balances: %v", err)
	}
	if balance.Balance != credit.Amount {
		t.Fatalf("%s balance = %d, want %d (the single faucet credit)",
			env.Market.QuoteSymbol, balance.Balance, credit.Amount)
	}

	// The session is a login session, not a minted one — only those can mint further tokens.
	if _, err := env.Client.MintToken(ctx, acc.LoginToken, "read"); err != nil {
		t.Fatalf("mint a token from the login session: %v", err)
	}
}
