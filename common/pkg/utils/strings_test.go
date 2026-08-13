package utils_test

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

func TestNilIfBlank(t *testing.T) {
	cases := []struct {
		in   string
		want *string
	}{
		{"", nil},
		{"   ", nil},
		{"\t\n", nil},
		{"hello", strPtr("hello")},
		{"  hello  ", strPtr("hello")},
	}
	for _, c := range cases {
		got := utils.NilIfBlank(c.in)
		if (got == nil) != (c.want == nil) {
			t.Errorf("NilIfBlank(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		if got != nil && *got != *c.want {
			t.Errorf("NilIfBlank(%q) = %q, want %q", c.in, *got, *c.want)
		}
	}
}

func strPtr(s string) *string { return &s }
