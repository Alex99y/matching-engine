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

func TestDefaultIfBlank(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"value is kept", "ETH-USDT", "ETH-USDT"},
		{"empty takes the fallback", "", "-"},
		{"whitespace counts as blank", "   ", "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.DefaultIfBlank(tt.in, "-"); got != tt.want {
				t.Fatalf("DefaultIfBlank(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatUint64PtrOr(t *testing.T) {
	v := uint64(1000)
	if got := utils.FormatUint64PtrOr(&v, "-"); got != "1000" {
		t.Fatalf("got %q, want 1000", got)
	}
	if got := utils.FormatUint64PtrOr(nil, "-"); got != "-" {
		t.Fatalf("nil got %q, want the fallback", got)
	}
	// Zero is a real value, not an absence — it must not be confused with nil.
	zero := uint64(0)
	if got := utils.FormatUint64PtrOr(&zero, "-"); got != "0" {
		t.Fatalf("zero got %q, want 0", got)
	}
}
