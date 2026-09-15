package stream

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

// The SSE handlers themselves hold a live connection open and are left to integration testing. This
// covers the one piece that is pure: the query-param gate in front of them.

// interval comes straight off the query string and is used to pick a candle bucket, so anything the
// allowed set does not name has to be refused rather than silently bucketed somewhere.
func TestParseIntervalAcceptsOnlyTheAllowedSet(t *testing.T) {
	allowed := map[int64]struct{}{60: {}, 300: {}, 900: {}, 3600: {}, 14400: {}, 86400: {}}

	for want := range allowed {
		raw := strconv.FormatInt(want, 10)
		t.Run("allows "+raw, func(t *testing.T) {
			got, err := parseInterval(raw, allowed)
			if err != nil {
				t.Fatalf("parseInterval(%q) = %v", raw, err)
			}
			if got != want {
				t.Fatalf("parseInterval(%q) = %d, want %d", raw, got, want)
			}
		})
	}

	rejected := []struct {
		name string
		raw  string
	}{
		{"missing", ""},
		{"not a number", "abc"},
		{"a plausible but unlisted interval", "120"},
		{"zero", "0"},
		{"negative", "-60"},
		{"float", "60.0"},
		{"whitespace padded", " 60 "},
		{"suffixed", "60s"},
		{"overflows int64", "99999999999999999999"},
		{"hex", "0x3c"},
	}

	for _, tt := range rejected {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			got, err := parseInterval(tt.raw, allowed)
			if !errors.Is(err, errInvalidInterval) {
				t.Fatalf("parseInterval(%q) err = %v, want errInvalidInterval", tt.raw, err)
			}
			if got != 0 {
				t.Fatalf("a rejected interval returned %d, want 0", got)
			}
		})
	}
}

// strconv.ParseInt accepts an explicit sign, so "+60" is the number 60 and passes the allow-set
// check like any other spelling of it. Harmless — it resolves to a listed interval, not past one —
// but pinned so the leniency is a decision rather than a surprise.
func TestParseIntervalAcceptsAnExplicitlySignedNumber(t *testing.T) {
	got, err := parseInterval("+60", map[int64]struct{}{60: {}})
	if err != nil {
		t.Fatalf("parseInterval(\"+60\") = %v", err)
	}
	if got != 60 {
		t.Fatalf("parseInterval(\"+60\") = %d, want 60", got)
	}
}

// A market with no configured intervals must reject everything rather than fall open.
func TestParseIntervalRejectsEverythingWhenNothingIsAllowed(t *testing.T) {
	for _, raw := range []string{"60", "3600", strconv.FormatInt(math.MaxInt64, 10)} {
		if _, err := parseInterval(raw, map[int64]struct{}{}); !errors.Is(err, errInvalidInterval) {
			t.Fatalf("parseInterval(%q) with an empty allow-set = %v", raw, err)
		}
	}
}
