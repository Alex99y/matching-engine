//go:build e2e

// Package auth covers the account lifecycle against a live stack: registration, login,
// session scoping, refresh, revocation, and the account freeze. One scenario per file; the
// catalog these map to is PLAN.md §3 (tests/auth).
package auth

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
