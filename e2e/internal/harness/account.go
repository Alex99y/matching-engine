package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// testPassword is shared by every account the suite creates — these are disposable sandbox
// accounts. It is >= 10 chars (the API minimum).
const testPassword = "e2e-test-password"

type Account struct {
	Username   string
	LoginToken string // write-scoped login session token
}

// NewAccount registers a fresh random account (username prefixed "e2e-", so seeded/real data
// is never touched and leftovers are identifiable) and logs it in.
func NewAccount(ctx context.Context, c *client.Client) (*Account, error) {
	name := "e2e-" + randSuffix()
	if err := c.Register(ctx, name, name+"@e2e.local", testPassword); err != nil && !errors.Is(err, client.ErrUsernameTaken) {
		return nil, fmt.Errorf("register %s: %w", name, err)
	}
	token, err := c.Login(ctx, name, testPassword)
	if err != nil {
		return nil, fmt.Errorf("login %s: %w", name, err)
	}
	return &Account{Username: name, LoginToken: token}, nil
}

// Fund adds `calls` faucet credits of symbol to the account.
func (a *Account) Fund(ctx context.Context, c *client.Client, symbol string, calls int) error {
	for i := 0; i < calls; i++ {
		if _, err := c.RequestFunds(ctx, a.LoginToken, symbol); err != nil {
			return fmt.Errorf("fund %s with %s (call %d/%d): %w", a.Username, symbol, i+1, calls, err)
		}
	}
	return nil
}

// FundMarket funds both legs of a market's pair with `calls` credits each.
func (a *Account) FundMarket(ctx context.Context, c *client.Client, m MarketRules, calls int) error {
	for _, symbol := range []string{m.BaseSymbol, m.QuoteSymbol} {
		if err := a.Fund(ctx, c, symbol, calls); err != nil {
			return err
		}
	}
	return nil
}

// ReadToken mints a read-scoped token from this account's login session — for tests that
// need to prove a read token is rejected on write routes.
func (a *Account) ReadToken(ctx context.Context, c *client.Client) (string, error) {
	minted, err := c.MintToken(ctx, a.LoginToken, "read")
	if err != nil {
		return "", fmt.Errorf("mint read token for %s: %w", a.Username, err)
	}
	return minted.Token, nil
}

func randSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("e2e: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
