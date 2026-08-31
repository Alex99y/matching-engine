//go:build e2e

package marketdata

import (
	"net/http"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// M1 — the market catalogue is complete and self-consistent.
//
// Lists every market, then fetches the one under test on its own.
// Expect: the single fetch matches the listing entry field for field, every market names two
// different instruments and carries a usable trading grid, and an unknown ref is a 404.
func TestMarketListingAndLookupAgree(t *testing.T) {
	ctx := env.Context(t)

	markets, err := env.Client.ListMarkets(ctx)
	if err != nil {
		t.Fatalf("list markets: %v", err)
	}
	if len(markets) == 0 {
		t.Fatal("no markets served — the stack was started before its seed data existed")
	}

	var listed *client.MarketInfo
	for i, m := range markets {
		if m.BaseSymbol+"-"+m.QuoteSymbol == env.Market.Ref {
			listed = &markets[i]
		}
	}
	if listed == nil {
		t.Fatalf("market %s under test is missing from the listing", env.Market.Ref)
	}

	// Only the market under test is held to these — the suite may be pointed at a database
	// carrying markets someone else created, and their configuration is not this test's
	// business.
	if listed.BaseSymbol == listed.QuoteSymbol {
		t.Fatalf("market %s trades an instrument against itself", env.Market.Ref)
	}
	// A zero quantum leaves no grid to round to, and a maximum below the minimum means the
	// market can never accept an order at all.
	if listed.PriceQuantum == 0 || listed.AmountQuantum == 0 {
		t.Fatalf("market %s has a zero quantum: price=%d amount=%d",
			env.Market.Ref, listed.PriceQuantum, listed.AmountQuantum)
	}
	if listed.MaxOrderSize < listed.MinOrderSize {
		t.Fatalf("market %s accepts nothing: max_order_size=%d < min_order_size=%d",
			env.Market.Ref, listed.MaxOrderSize, listed.MinOrderSize)
	}

	fetched, err := env.Client.GetMarket(ctx, env.Market.Ref)
	if err != nil {
		t.Fatalf("get market %s: %v", env.Market.Ref, err)
	}
	if fetched != *listed {
		t.Fatalf("single fetch disagrees with the listing:\n  get  = %+v\n  list = %+v", fetched, *listed)
	}
}

// M1 — an unknown market ref is a 404, not an empty success.
func TestUnknownMarketIsNotFound(t *testing.T) {
	ctx := env.Context(t)

	for _, ref := range []string{"NOPE-NOPE", "ETH-NOPE", "NOPE"} {
		t.Run(ref, func(t *testing.T) {
			if _, err := env.Client.GetMarket(ctx, ref); err == nil {
				t.Fatalf("GET /markets/%s succeeded, want 404", ref)
			} else if got := client.Status(err); got != http.StatusNotFound {
				t.Fatalf("GET /markets/%s status = %d, want %d — %v", ref, got, http.StatusNotFound, err)
			}
			if _, err := env.Client.GetDepth(ctx, ref, 0); err == nil {
				t.Fatalf("GET /markets/%s/depth succeeded, want 404", ref)
			}
		})
	}
}

// M1 — the market's instruments resolve, and their decimals are what the suite scales by.
//
// Expect: both legs of the market under test are listed as instruments, a direct lookup
// agrees with the listing, and the decimals match the ones the harness resolved — a mismatch
// would silently corrupt every amount the suite asserts on.
func TestInstrumentsBackTheMarket(t *testing.T) {
	ctx := env.Context(t)

	instruments, err := env.Client.ListInstruments(ctx)
	if err != nil {
		t.Fatalf("list instruments: %v", err)
	}

	bySymbol := make(map[string]client.Instrument, len(instruments))
	for _, i := range instruments {
		bySymbol[i.Symbol] = i
	}

	for _, leg := range []struct {
		symbol   string
		decimals int
	}{
		{env.Market.BaseSymbol, env.Market.BaseDecimals},
		{env.Market.QuoteSymbol, env.Market.QuoteDecimals},
	} {
		listed, ok := bySymbol[leg.symbol]
		if !ok {
			t.Fatalf("instrument %s backs market %s but is not listed", leg.symbol, env.Market.Ref)
		}
		if listed.Decimals != leg.decimals {
			t.Fatalf("%s decimals = %d, but the harness scaled by %d",
				leg.symbol, listed.Decimals, leg.decimals)
		}

		fetched, err := env.Client.GetInstrument(ctx, leg.symbol)
		if err != nil {
			t.Fatalf("get instrument %s: %v", leg.symbol, err)
		}
		if fetched != listed {
			t.Fatalf("single fetch of %s disagrees with the listing:\n  get  = %+v\n  list = %+v",
				leg.symbol, fetched, listed)
		}
	}

	if _, err := env.Client.GetInstrument(ctx, "NOPE"); err == nil {
		t.Fatal("GET /instruments/NOPE succeeded, want 404")
	} else if got := client.Status(err); got != http.StatusNotFound {
		t.Fatalf("unknown instrument status = %d, want %d", got, http.StatusNotFound)
	}

	// BaseScale is what every notional in the suite divides by; it must be 10^decimals.
	var want uint64 = 1
	for i := 0; i < env.Market.BaseDecimals; i++ {
		want *= 10
	}
	if env.Market.BaseScale != want {
		t.Fatalf("BaseScale = %d, want 10^%d = %d", env.Market.BaseScale, env.Market.BaseDecimals, want)
	}
}
