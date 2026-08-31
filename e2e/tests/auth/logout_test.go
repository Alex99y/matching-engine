//go:build e2e

package auth

import (
	"net/http"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// A6 — logging out revokes exactly one session.
//
// Opens two login sessions for the same account and logs the first one out.
// Expect: the logged-out token is refused with 401 from then on, the second token keeps
// working, and GET /sessions/active drops from two entries to one.
func TestLogoutRevokesOnlyItsOwnSession(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t)

	second, err := env.Client.Login(ctx, acc.Username, acc.Password)
	if err != nil {
		t.Fatalf("open a second session: %v", err)
	}

	sessions, err := env.Client.ListSessions(ctx, acc.LoginToken)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("account has %d active session(s), want 2", len(sessions))
	}

	if err := env.Client.Logout(ctx, acc.LoginToken); err != nil {
		t.Fatalf("logout: %v", err)
	}

	if _, err := env.Client.GetBalances(ctx, acc.LoginToken); err == nil {
		t.Fatal("the logged-out token still authenticates, want 401")
	} else if got := client.Status(err); got != http.StatusUnauthorized {
		t.Fatalf("logged-out token status = %d, want %d — %v", got, http.StatusUnauthorized, err)
	}

	remaining, err := env.Client.ListSessions(ctx, second)
	if err != nil {
		t.Fatalf("the second session stopped working after the first logged out: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("account has %d active session(s) after one logout, want 1", len(remaining))
	}
}
