package orderbook

import (
	"math"
	"testing"
)

// This file covers the package's scaled-quote arithmetic at its edges — degenerate inputs
// (zero price, zero scale) and the 128-bit overflow boundary — none of which a test driving
// MatchOrder can reach, because ValidateOrderEvent rejects such orders upstream.

func TestQuoteAmount(t *testing.T) {
	cases := []struct {
		name                  string
		price, qty, baseScale uint64
		want                  uint64
	}{
		{"unscaled market", 100, 10, 1, 1000},
		{"scaled market", 2000, 5000, 1000, 10000},
		{"zero base scale is treated as unscaled", 100, 10, 0, 1000},
		// price*qty is 2^80 here: it wraps to 0 in a bare uint64 multiply, which is the whole
		// reason this function carries a 128-bit intermediate.
		{"product exceeds uint64 but the result fits", 1 << 40, 1 << 40, 1 << 40, 1 << 40},
		{"saturates instead of wrapping", math.MaxUint64, math.MaxUint64, 1, math.MaxUint64},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteAmount(c.price, c.qty, c.baseScale); got != c.want {
				t.Fatalf("quoteAmount(%d, %d, %d) = %d, want %d", c.price, c.qty, c.baseScale, got, c.want)
			}
		})
	}
}

func TestAffordableBase(t *testing.T) {
	cases := []struct {
		name                    string
		quote, price, baseScale uint64
		want                    uint64
	}{
		{"unscaled market", 1000, 100, 1, 10},
		{"budget below one whole coin's price", 500, 2000, 1000, 250},
		{"zero price buys nothing", 1000, 0, 1, 0},
		{"zero base scale is treated as unscaled", 1000, 10, 0, 100},
		{"saturates instead of wrapping", math.MaxUint64, 1, 2, math.MaxUint64},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := affordableBase(c.quote, c.price, c.baseScale); got != c.want {
				t.Fatalf("affordableBase(%d, %d, %d) = %d, want %d", c.quote, c.price, c.baseScale, got, c.want)
			}
		})
	}
}

// affordableBase and quoteAmount are inverses, and a market buy relies on that: it converts its
// budget to a quantity, then charges itself the cost of that quantity. Integer division may
// lose a quantum on the way, but it must never manufacture one and overspend the reservation.
func TestAffordableBaseRoundTripNeverExceedsBudget(t *testing.T) {
	const baseScale = 1_000
	for _, price := range []uint64{1, 7, 100, 2000, 999_983} {
		for _, budget := range []uint64{1, 3, 500, 1001, 1_000_000} {
			base := affordableBase(budget, price, baseScale)
			if cost := quoteAmount(price, base, baseScale); cost > budget {
				t.Fatalf("price=%d budget=%d: affords %d base costing %d, over budget", price, budget, base, cost)
			}
		}
	}
}
