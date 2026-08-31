//go:build e2e

// Package streams covers the SSE feeds: the private order stream, the public market stream,
// and the candle stream. One scenario per file; the catalog these map to is PLAN.md §3
// (tests/streams).
//
// None of these feeds replay, so every test connects before the action it wants to observe.
// The public feeds are market-wide, so assertions filter to this test's own price band rather
// than assuming the market is idle.
package streams

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
