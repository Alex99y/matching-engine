package utils

import "math"

// Pow10Uint64 returns 10^n as a uint64. Panics if n > 19 (would overflow uint64).
func Pow10Uint64(n int) uint64 {
	if n > 19 {
		panic("Pow10Uint64: exponent exceeds uint64 range")
	}
	return uint64(math.Pow10(n))
}
