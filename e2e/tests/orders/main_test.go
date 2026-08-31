//go:build e2e

// Package orders covers order entry and settlement against a live stack: what rests, what
// trades, what is refused, and where the money ends up. One scenario per file; the catalog
// these map to is PLAN.md §3 (tests/orders).
package orders

import (
	"context"
	"os"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/harness"
	"github.com/alex99y/matching-engine/e2e/internal/suite"
)

var env *suite.Env

func TestMain(m *testing.M) {
	env = suite.Setup()
	os.Exit(m.Run())
}

// Thin aliases over the shared scenario helpers on suite.Env, so the tests below read as
// prose. Each test claims its own price band via band(t) — see suite.Env.Band.
func band(t *testing.T) uint64 { return env.Band(t) }
func minQty() uint64           { return env.MinQty() }

func send(t *testing.T, ctx context.Context, acc *harness.Account, o client.NewOrder) string {
	t.Helper()
	return env.Send(t, ctx, acc, o)
}

func rest(t *testing.T, ctx context.Context, acc *harness.Account, o client.NewOrder) string {
	t.Helper()
	return env.Rest(t, ctx, acc, o)
}

func fetch(t *testing.T, ctx context.Context, token, orderID string) client.Order {
	t.Helper()
	return env.Fetch(t, ctx, token, orderID)
}

func snapshot(t *testing.T, ctx context.Context, token string) assert.Balances {
	t.Helper()
	return env.Snapshot(t, ctx, token)
}

func diffAgainst(t *testing.T, ctx context.Context, token string, before assert.Balances) map[string]assert.Move {
	t.Helper()
	return env.DiffAgainst(t, ctx, token, before)
}

func containsOrder(orders []client.Order, id string) bool {
	for _, o := range orders {
		if o.ID == id {
			return true
		}
	}
	return false
}
