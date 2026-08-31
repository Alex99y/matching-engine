package fixtures

import "testing"

func TestToRaw(t *testing.T) {
	cases := []struct {
		dec      string
		decimals int
		want     uint64
	}{
		{"2000", 6, 2_000_000_000},
		{"0.169", 9, 169_000_000},
		{"1.5", 18, 1_500_000_000_000_000_000},
		{"2,000.50", 6, 2_000_500_000},
		{"0", 6, 0},
		{"7", 0, 7},
	}
	for _, c := range cases {
		if got := ToRaw(c.dec, c.decimals); got != c.want {
			t.Errorf("ToRaw(%q, %d) = %d, want %d", c.dec, c.decimals, got, c.want)
		}
	}
}

func TestToRawPanicsOnBadInput(t *testing.T) {
	for _, bad := range []struct {
		dec      string
		decimals int
	}{
		{"-1", 6},
		{"", 6},
		{"1.2345", 2}, // too many fractional digits
		{"abc", 6},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ToRaw(%q, %d) did not panic", bad.dec, bad.decimals)
				}
			}()
			ToRaw(bad.dec, bad.decimals)
		}()
	}
}

func TestFromRaw(t *testing.T) {
	cases := []struct {
		raw      uint64
		decimals int
		want     string
	}{
		{2_000_000_000, 6, "2000"},
		{169_000_000, 9, "0.169"},
		{1_500_000_000_000_000_000, 18, "1.5"},
		{7, 0, "7"},
		{0, 6, "0"},
	}
	for _, c := range cases {
		if got := FromRaw(c.raw, c.decimals); got != c.want {
			t.Errorf("FromRaw(%d, %d) = %q, want %q", c.raw, c.decimals, got, c.want)
		}
	}
}

func TestToFromRawRoundTrip(t *testing.T) {
	for _, dec := range []string{"1", "0.001", "12345.6789", "999999"} {
		if got := FromRaw(ToRaw(dec, 9), 9); got != dec {
			t.Errorf("round trip %q → %q", dec, got)
		}
	}
}
