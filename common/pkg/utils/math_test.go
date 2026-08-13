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
