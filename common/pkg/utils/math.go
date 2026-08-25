package utils

import (
	"math"
	"strconv"
)

// Pow10Uint64 returns 10^n as a uint64. Panics if n > 19 (would overflow uint64).
func Pow10Uint64(n int) uint64 {
	if n > 19 {
		panic("Pow10Uint64: exponent exceeds uint64 range")
	}
	return uint64(math.Pow10(n))
}

// PercentChangePtr returns the percentage change from open to current, formatted to 2 decimals
// (e.g. "-3.14"). Returns nil if either value is unknown or open is 0 (division guard) — a
// display-only stat, so float64 is fine here even though price/qty stay integer everywhere else.
func PercentChangePtr(current, open *uint64) *string {
	if current == nil || open == nil || *open == 0 {
		return nil
	}
	pct := (float64(*current) - float64(*open)) / float64(*open) * 100
	formatted := strconv.FormatFloat(pct, 'f', 2, 64)
	return &formatted
}
