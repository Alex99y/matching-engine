//go:build e2e

package marketdata

import (
	"context"
	"strconv"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// M5 — the 24-hour ticker follows the tape.
//
// Reads /markets/prices, trades, and reads it again.
// Expect: the market under test is listed, its last price becomes the price just traded, the
// 24h volume grows by the traded quantity, and the traded price sits inside the 24h high/low
// band. The stats are market-wide, so everything here is asserted as a delta.
func TestPricesFollowTheLastTrade(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	before := priceOf(t, ctx, env.Market.Ref)
	volumeBefore := optionalAmount(t, before.Volume24h)

	price, qty := env.Band(t), env.MinQty()
	env.Trade(t, ctx, maker, taker, price, qty)

	var after client.MarketPrice
	assert.Eventually(t, ctx, func() error {
		after = priceOf(t, ctx, env.Market.Ref)
		if after.Price == nil {
			return errNoPriceYet{}
		}
		// This assumes nothing else trades this market meanwhile — true while the suite runs
		// serially against a stack with no bots attached.
		if got := parseAmount(t, *after.Price); got != price {
			return errPriceStale{got: got, want: price}
		}
		return nil
	})

	// Volume is cumulative over the window, so it can only have grown by at least our fill.
	volumeAfter := optionalAmount(t, after.Volume24h)
	if volumeAfter < volumeBefore+qty {
		t.Fatalf("24h volume went %d → %d, want at least +%d from this trade",
			volumeBefore, volumeAfter, qty)
	}

	// The price just traded has to be inside the window's own high/low band.
	if after.MinPrice24h == nil || after.MaxPrice24h == nil {
		t.Fatalf("a traded market reports no 24h range: min=%v max=%v", after.MinPrice24h, after.MaxPrice24h)
	}
	low, high := parseAmount(t, *after.MinPrice24h), parseAmount(t, *after.MaxPrice24h)
	if price < low || price > high {
		t.Fatalf("traded at %d, outside the reported 24h range %d-%d", price, low, high)
	}
	if low > high {
		t.Fatalf("24h range is inverted: min=%d max=%d", low, high)
	}

	// change_percent_24h is pre-formatted server-side; it is a display string, not an amount.
	if after.ChangePercent24h == nil {
		t.Fatal("a traded market reports no 24h change")
	}
	if _, err := strconv.ParseFloat(*after.ChangePercent24h, 64); err != nil {
		t.Fatalf("change_percent_24h = %q, want a decimal string: %v", *after.ChangePercent24h, err)
	}
}

// M5 — every served market appears in the ticker, traded or not.
func TestPricesCoverEveryMarket(t *testing.T) {
	ctx := env.Context(t)

	markets, err := env.Client.ListMarkets(ctx)
	if err != nil {
		t.Fatalf("list markets: %v", err)
	}
	prices, err := env.Client.GetPrices(ctx)
	if err != nil {
		t.Fatalf("get prices: %v", err)
	}

	listed := make(map[string]struct{}, len(prices))
	for _, p := range prices {
		listed[p.Market] = struct{}{}
	}
	for _, m := range markets {
		ref := m.BaseSymbol + "-" + m.QuoteSymbol
		if _, ok := listed[ref]; !ok {
			t.Fatalf("market %s is served but missing from /markets/prices", ref)
		}
	}
}

func priceOf(t *testing.T, ctx context.Context, ref string) client.MarketPrice {
	t.Helper()

	prices, err := env.Client.GetPrices(ctx)
	if err != nil {
		t.Fatalf("get prices: %v", err)
	}
	for _, p := range prices {
		if p.Market == ref {
			return p
		}
	}
	t.Fatalf("market %s missing from /markets/prices", ref)
	return client.MarketPrice{}
}

// optionalAmount reads a nullable decimal-string amount, treating absent as zero — a market
// that has not traded in the window reports null rather than "0".
func optionalAmount(t *testing.T, s *string) uint64 {
	t.Helper()

	if s == nil {
		return 0
	}
	return parseAmount(t, *s)
}

type errNoPriceYet struct{}

func (errNoPriceYet) Error() string { return "market has no last price yet" }

type errPriceStale struct{ got, want uint64 }

func (e errPriceStale) Error() string {
	return "last price " + strconv.FormatUint(e.got, 10) + ", waiting for " + strconv.FormatUint(e.want, 10)
}
