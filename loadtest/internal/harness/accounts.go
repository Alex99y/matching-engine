// Package harness provisions the accounts and market context every e2e test needs, so the
// individual cmd binaries only contain the measurement scenario itself.
package harness

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/loadtest/internal/client"
)

// testAccountPassword is shared by every account this suite creates. It never needs to be secret
// — these are throwaway sandbox accounts identifiable by the account-test- prefix.
const testAccountPassword = "e2e-load-test-pw"

const defaultFundingCalls = 50

type Account struct {
	Username string
	Password string
	Token    string
}

func MakerAccountName(i int) string { return fmt.Sprintf("account-test-mkr-%d", i) }
func TakerAccountName(i int) string { return fmt.Sprintf("account-test-tkr-%d", i) }
func MeasuredAccountName(testName string) string {
	return fmt.Sprintf("act-test-%s", testName)
}

// EnsureAccount registers username if it doesn't already exist (a prior run of this suite likely
// already created it — that's expected, not an error) and logs in, returning a ready-to-use
// write-scoped token.
func EnsureAccount(ctx context.Context, c *client.Client, username string) (*Account, error) {
	email := username + "@e2e.local"
	err := c.Register(ctx, username, email, testAccountPassword)
	if err != nil && !errors.Is(err, client.ErrUsernameTaken) {
		return nil, fmt.Errorf("register %s: %w", username, err)
	}

	token, err := c.Login(ctx, username, testAccountPassword)
	if err != nil {
		return nil, fmt.Errorf("login %s: %w", username, err)
	}

	return &Account{Username: username, Password: testAccountPassword, Token: token}, nil
}

// Fund tops up acc with `calls` faucet credits per symbol. The faucet has no rate limit or cap
// (api/internal/faucet/service.go), so this is just a loop — done generously up front so a long
// run doesn't stall mid-test on insufficient balance.
func Fund(ctx context.Context, c *client.Client, acc *Account, symbols []string, calls int) error {
	for _, symbol := range symbols {
		for i := 0; i < calls; i++ {
			if _, err := c.RequestFunds(ctx, acc.Token, symbol); err != nil {
				return fmt.Errorf("fund %s with %s: %w", acc.Username, symbol, err)
			}
		}
	}
	return nil
}
