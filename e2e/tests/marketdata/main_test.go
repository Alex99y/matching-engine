//go:build e2e

// Package marketdata covers the public read side: what the API publishes about markets,
// instruments, the book, and the tape. One scenario per file; the catalog these map to is
// PLAN.md §3 (tests/marketdata).
//
// These endpoints are market-wide, so nothing here may assume the market is otherwise idle.
// Assertions are either delta-based (prices, matches) or scoped to a time window / price band
// this test alone occupies.
package marketdata

import (
	"os"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/suite"
)

var env *suite.Env

func TestMain(m *testing.M) {
	env = suite.Setup()
	os.Exit(m.Run())
}
