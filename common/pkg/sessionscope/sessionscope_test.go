package sessionscope_test

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/sessionscope"
)

// These constants back CHECK constraints and comparisons elsewhere in the codebase
// (e.g. the sessions table, session-scope gating) — a silent literal change here would
// break those without touching this package at all, so pin the exact values.
func TestConstants(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"OriginLogin", sessionscope.OriginLogin, "login"},
		{"OriginMinted", sessionscope.OriginMinted, "minted"},
		{"ScopeRead", sessionscope.ScopeRead, "read"},
		{"ScopeWrite", sessionscope.ScopeWrite, "write"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
