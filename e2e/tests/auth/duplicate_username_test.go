//go:build e2e

package auth

import (
	"errors"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/google/uuid"
)

// A2 — usernames are unique.
//
// Registers an account, then tries to register the same username again.
// Expect: the second attempt is refused with 409 (surfaced as ErrUsernameTaken), and
// POST /users/check-username reports the name as taken while an unused one stays available.
func TestDuplicateUsernameIsRejected(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewAccount(t)

	err := env.Client.Register(ctx, acc.Username, acc.Username+"@e2e.local", acc.Password)
	if !errors.Is(err, client.ErrUsernameTaken) {
		t.Fatalf("re-registering %q: err = %v (status %d), want ErrUsernameTaken (409)",
			acc.Username, err, client.Status(err))
	}

	taken, err := env.Client.CheckUsername(ctx, acc.Username)
	if err != nil {
		t.Fatalf("check-username for a registered name: %v", err)
	}
	if taken {
		t.Fatalf("username %q reported available after registration", acc.Username)
	}

	// A name nobody has claimed must still read as available, so the check is not simply
	// answering "unavailable" to everything. Usernames are capped at 25 characters, so build
	// a fresh short one rather than extending the account's.
	free := "e2e-free-" + uuid.NewString()[:12]
	available, err := env.Client.CheckUsername(ctx, free)
	if err != nil {
		t.Fatalf("check-username for an unused name: %v", err)
	}
	if !available {
		t.Fatalf("unused username %q reported unavailable", free)
	}
}
