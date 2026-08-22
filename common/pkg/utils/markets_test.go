package utils_test

import (
	"errors"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

func TestSplitMarketRef(t *testing.T) {
	cases := []struct {
		ref       string
		wantBase  string
		wantQuote string
		wantErr   error
	}{
		{"BTC-USDT", "BTC", "USDT", nil},
		{"ETH-BTC", "ETH", "BTC", nil},
		{"BTC-USD-T", "BTC", "USD-T", nil}, // SplitN(2): only the first "-" separates
		{"BTC-", "", "", utils.ErrInvalidMarketRef},
		{"-USDT", "", "", utils.ErrInvalidMarketRef},
		{"BTCUSDT", "", "", utils.ErrInvalidMarketRef},
		{"", "", "", utils.ErrInvalidMarketRef},
	}
	for _, c := range cases {
		base, quote, err := utils.SplitMarketRef(c.ref)
		if base != c.wantBase || quote != c.wantQuote || !errors.Is(err, c.wantErr) {
			t.Errorf("SplitMarketRef(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.ref, base, quote, err, c.wantBase, c.wantQuote, c.wantErr)
		}
	}
}

func TestMergeMarketRef(t *testing.T) {
	if got := utils.MergeMarketRef("BTC", "USDT"); got != "BTC-USDT" {
		t.Errorf("MergeMarketRef() = %q, want %q", got, "BTC-USDT")
	}
}

func TestSplitMergeRoundTrip(t *testing.T) {
	base, quote, err := utils.SplitMarketRef(utils.MergeMarketRef("ETH", "USDT"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "ETH" || quote != "USDT" {
		t.Errorf("round trip = (%q, %q), want (\"ETH\", \"USDT\")", base, quote)
	}
}

func TestParsePriceGroup(t *testing.T) {
	cases := []struct {
		raw          string
		priceQuantum uint64
		want         uint64
		wantErr      error
	}{
		{"", 5, 5, nil},         // empty -> native resolution
		{"5", 5, 5, nil},        // == quantum
		{"25", 5, 25, nil},      // multiple of quantum
		{"0", 5, 0, utils.ErrInvalidPriceGroup},
		{"7", 5, 0, utils.ErrInvalidPriceGroup},  // not a multiple
		{"-1", 5, 0, utils.ErrInvalidPriceGroup}, // not a valid uint64
		{"abc", 5, 0, utils.ErrInvalidPriceGroup},
		{"", 0, 1, nil}, // defensive: zero quantum treated as 1
	}
	for _, c := range cases {
		got, err := utils.ParsePriceGroup(c.raw, c.priceQuantum)
		if got != c.want || !errors.Is(err, c.wantErr) {
			t.Errorf("ParsePriceGroup(%q, %d) = (%d, %v), want (%d, %v)",
				c.raw, c.priceQuantum, got, err, c.want, c.wantErr)
		}
	}
}
