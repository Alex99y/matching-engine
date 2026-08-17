package utils_test

import (
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/utils"
)

func strPtr(s string) *string { return &s }

func TestNilIfEmpty(t *testing.T) {
	cases := []struct {
		in   string
		want *string
	}{
		{"", nil},
		{"hello", strPtr("hello")},
		{" ", strPtr(" ")}, // unlike a "blank" check, a lone space is not empty
	}
	for _, c := range cases {
		got := utils.NilIfEmpty(c.in)
		if (got == nil) != (c.want == nil) {
			t.Errorf("NilIfEmpty(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		if got != nil && *got != *c.want {
			t.Errorf("NilIfEmpty(%q) = %q, want %q", c.in, *got, *c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 0, ""},
		{"", 3, ""},
		{"héllo", 3, "hél"}, // rune-safe: must not split a multi-byte character
	}
	for _, c := range cases {
		if got := utils.Truncate(c.in, c.n); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
