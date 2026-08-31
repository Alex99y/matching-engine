//go:build e2e

package auth

import (
	"net/http"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// A3 — login refuses bad credentials without disclosing whether the account exists.
//
// Tries a wrong password on a real account, and any password on a username that was never
// registered.
// Expect: both are refused with 401 and no token, so a caller cannot use the status code to
// enumerate usernames.
func TestLoginRejectsInvalidCredentials(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t)

	cases := []struct {
		name     string
		username string
		password string
	}{
		{"wrong password on an existing account", acc.Username, "not-the-password"},
		{"account that does not exist", acc.Username + "-nobody", acc.Password},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := env.Client.Login(ctx, tc.username, tc.password)
			if err == nil {
				t.Fatalf("login succeeded, want 401 (token %q)", token)
			}
			if got := client.Status(err); got != http.StatusUnauthorized {
				t.Fatalf("login status = %d, want %d — %v", got, http.StatusUnauthorized, err)
			}
			if token != "" {
				t.Fatalf("login returned token %q alongside an error", token)
			}
		})
	}

	// The credentials themselves are still good — the rejections above were about the
	// wrong password, not a broken account.
	if _, err := env.Client.Login(ctx, acc.Username, acc.Password); err != nil {
		t.Fatalf("login with correct credentials: %v", err)
	}
}
