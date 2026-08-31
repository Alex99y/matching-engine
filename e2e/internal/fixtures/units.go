package fixtures

import (
	"fmt"
	"strconv"
	"strings"
)

// ToRaw converts a human decimal string ("1.5", "2,000") to raw quanta at `decimals`. It
// panics on malformed input or more fractional digits than `decimals` — fixtures are
// test-authored constants, so a bad literal is a bug that should fail loudly. Mirrors
// parseUnits in ui/src/utils/format.ts.
func ToRaw(dec string, decimals int) uint64 {
	s := strings.ReplaceAll(strings.TrimSpace(dec), ",", "")
	if s == "" || strings.HasPrefix(s, "-") {
		panic(fmt.Sprintf("fixtures.ToRaw: not a non-negative decimal: %q", dec))
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if hasFrac && len(frac) > decimals {
		panic(fmt.Sprintf("fixtures.ToRaw(%q, %d): more than %d fractional digits", dec, decimals, decimals))
	}
	scale := pow10(decimals)

	w, err := strconv.ParseUint(orZero(whole), 10, 64)
	if err != nil {
		panic(fmt.Sprintf("fixtures.ToRaw(%q): %v", dec, err))
	}
	f, err := strconv.ParseUint(orZero(frac)+strings.Repeat("0", decimals-len(frac)), 10, 64)
	if err != nil {
		panic(fmt.Sprintf("fixtures.ToRaw(%q): %v", dec, err))
	}
	return w*scale + f
}

// FromRaw is the inverse of ToRaw, for readable assertion messages (trailing zeros trimmed).
func FromRaw(raw uint64, decimals int) string {
	if decimals == 0 {
		return strconv.FormatUint(raw, 10)
	}
	scale := pow10(decimals)
	whole, frac := raw/scale, raw%scale
	fracStr := strings.TrimRight(fmt.Sprintf("%0*d", decimals, frac), "0")
	if fracStr == "" {
		return strconv.FormatUint(whole, 10)
	}
	return fmt.Sprintf("%d.%s", whole, fracStr)
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func pow10(n int) uint64 {
	v := uint64(1)
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
