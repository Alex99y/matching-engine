package utils_test

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

func TestPow10Uint64(t *testing.T) {
	cases := []struct {
		n    int
		want uint64
	}{
		{0, 1},
		{1, 10},
		{6, 1_000_000},
		{18, 1_000_000_000_000_000_000},
		{19, 10_000_000_000_000_000_000},
	}
	for _, c := range cases {
		if got := utils.Pow10Uint64(c.n); got != c.want {
			t.Errorf("Pow10Uint64(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestPow10Uint64PanicsOnOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected Pow10Uint64(20) to panic")
		}
	}()
	utils.Pow10Uint64(20)
}

func TestPercentChangePtr(t *testing.T) {
	u := func(v uint64) *uint64 { return &v }

	cases := []struct {
		name          string
		current, open *uint64
		want          *string
	}{
		{"nil current", nil, u(100), nil},
		{"nil open", u(100), nil, nil},
		{"zero open", u(100), u(0), nil},
		{"unchanged", u(100), u(100), strPtr("0.00")},
		{"up", u(110), u(100), strPtr("10.00")},
		{"down", u(90), u(100), strPtr("-10.00")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := utils.PercentChangePtr(c.current, c.open)
			if (got == nil) != (c.want == nil) {
				t.Fatalf("PercentChangePtr() = %v, want %v", got, c.want)
			}
			if got != nil && *got != *c.want {
				t.Fatalf("PercentChangePtr() = %q, want %q", *got, *c.want)
			}
		})
	}
}
