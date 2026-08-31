//go:build e2e

package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// A6 — a login session can revoke another of the account's sessions by id.
//
// Opens a second session and revokes it from the first, using the session_id that
// GET /sessions/active reports for it.
// Expect: the revoked token is refused with 401, the revoking session survives, and a
// minted token is not allowed to revoke anything (RequireLoginOrigin → 403).
func TestRevokeAnotherSessionByID(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t)

	// The listing is per-account, not per-token, so capture the first session's id before
	// opening the second — afterwards the two are indistinguishable from the response alone.
	first, err := env.Client.ListSessions(ctx, acc.LoginToken)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("fresh account has %d active session(s), want 1", len(first))
	}
	firstID := first[0].SessionID

	victim, err := env.Client.Login(ctx, acc.Username, acc.Password)
	if err != nil {
		t.Fatalf("open a second session: %v", err)
	}

	victimID := otherSessionID(t, ctx, acc.LoginToken, firstID)

	// A minted token is a dead end: it may act within its scope but never touch a session.
	minted, err := acc.ReadToken(ctx, env.Client)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if err := env.Client.RevokeSession(ctx, minted, victimID); err == nil {
		t.Fatal("a minted token revoked a session, want 403")
	} else if got := client.Status(err); got != http.StatusForbidden {
		t.Fatalf("revoke from a minted token: status = %d, want %d — %v", got, http.StatusForbidden, err)
	}

	if err := env.Client.RevokeSession(ctx, acc.LoginToken, victimID); err != nil {
		t.Fatalf("revoke the second session: %v", err)
	}

	if _, err := env.Client.GetBalances(ctx, victim); err == nil {
		t.Fatal("the revoked token still authenticates, want 401")
	} else if got := client.Status(err); got != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d, want %d — %v", got, http.StatusUnauthorized, err)
	}

	if _, err := env.Client.GetBalances(ctx, acc.LoginToken); err != nil {
		t.Fatalf("the revoking session stopped working: %v", err)
	}
}

// otherSessionID returns the account's one active session whose id is not knownID.
func otherSessionID(t *testing.T, ctx context.Context, token, knownID string) string {
	t.Helper()

	sessions, err := env.Client.ListSessions(ctx, token)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	var found string
	for _, s := range sessions {
		if s.SessionID == knownID {
			continue
		}
		if found != "" {
			t.Fatalf("expected exactly one session besides %s, found several", knownID)
		}
		found = s.SessionID
	}
	if found == "" {
		t.Fatalf("no session besides %s among %d listed", knownID, len(sessions))
	}
	return found
}
