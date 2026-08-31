//go:build e2e

package auth

import (
	"testing"
	"time"
)

// A5 — refreshing extends the session in place.
//
// Calls POST /sessions/refresh on a live login session.
// Expect: a new expiry in the future, the *same* token still working afterwards, and no
// extra session created — refresh extends the session, it does not mint a replacement token.
func TestRefreshExtendsSessionWithoutMintingANewToken(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t)

	before, err := env.Client.ListSessions(ctx, acc.LoginToken)
	if err != nil {
		t.Fatalf("list sessions before refresh: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("fresh account has %d active session(s), want 1", len(before))
	}

	expiresAt, err := env.Client.RefreshSession(ctx, acc.LoginToken)
	if err != nil {
		t.Fatalf("refresh session: %v", err)
	}
	if expiresAt <= time.Now().Unix() {
		t.Fatalf("refreshed expires_at = %d, want a time in the future", expiresAt)
	}
	if expiresAt < before[0].ExpiresAt {
		t.Fatalf("refreshed expires_at = %d, earlier than the original %d", expiresAt, before[0].ExpiresAt)
	}

	// Same token, still valid.
	if _, err := env.Client.GetBalances(ctx, acc.LoginToken); err != nil {
		t.Fatalf("original token after refresh: %v", err)
	}

	after, err := env.Client.ListSessions(ctx, acc.LoginToken)
	if err != nil {
		t.Fatalf("list sessions after refresh: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("account has %d active session(s) after refresh, want 1", len(after))
	}
	if after[0].SessionID != before[0].SessionID {
		t.Fatalf("session id changed on refresh (%s → %s); refresh must extend, not replace",
			before[0].SessionID, after[0].SessionID)
	}
}
