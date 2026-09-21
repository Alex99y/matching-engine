package utils

import "math/bits"

// WithinPercent reports whether value lies within ±percent of reference, bounds included. Both
// sides of the comparison are 128-bit products, so neither a reference near the uint64 maximum
// nor a percent above 100 can overflow or panic.
func WithinPercent(value, reference, percent uint64) bool {
	deviation := value - reference
	if value < reference {
		deviation = reference - value
	}
	devHi, devLo := bits.Mul64(deviation, 100)
	tolHi, tolLo := bits.Mul64(reference, percent)
	return devHi < tolHi || (devHi == tolHi && devLo <= tolLo)
}
