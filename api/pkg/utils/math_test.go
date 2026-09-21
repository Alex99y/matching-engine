package utils_test

import (
	"math"
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/utils"
)

func TestWithinPercent(t *testing.T) {
	tests := []struct {
		name                      string
		value, reference, percent uint64
		want                      bool
	}{
		{"equal", 80_000, 80_000, 10, true},
		{"upper bound inclusive", 88_000, 80_000, 10, true},
		{"lower bound inclusive", 72_000, 80_000, 10, true},
		{"one above the band", 88_001, 80_000, 10, false},
		{"one below the band", 71_999, 80_000, 10, false},
		{"zero percent accepts only the reference", 80_001, 80_000, 0, false},
		{"zero percent accepts the reference", 80_000, 80_000, 0, true},
		{"band wider than the reference reaches zero", 0, 80_000, 100, true},
		{"percent above 100 does not overflow", math.MaxUint64, 1, 200, false},
		{"reference near the maximum does not overflow", math.MaxUint64 - 1, math.MaxUint64, 10, true},
		{"reference near the maximum still bounds", math.MaxUint64 / 2, math.MaxUint64, 10, false},
		// 10% of 7 is 0.7: the exact comparison keeps 8 outside, where a truncated tolerance
		// (7/10 = 0) would also, but 6 stays inside a band that truncation would have closed.
		{"no truncation below one tick", 6, 7, 15, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.WithinPercent(tt.value, tt.reference, tt.percent); got != tt.want {
				t.Fatalf("WithinPercent(%d, %d, %d) = %v, want %v", tt.value, tt.reference, tt.percent, got, tt.want)
			}
		})
	}
}
