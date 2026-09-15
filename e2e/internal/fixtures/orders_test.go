package fixtures

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/harness"
)

// An 18-decimal base makes price × qty overflow uint64 at any realistic price. Both helpers
// must go through a wide intermediate, or every ETH-USDT notional in the suite is garbage —
// which is what silently happened before this test existed.
func TestNotionalAndTradablePriceSurviveAnEighteenDecimalBase(t *testing.T) {
	eth := harness.MarketRules{
		BaseScale:    1_000_000_000_000_000_000, // 1e18
		PriceQuantum: 1, AmountQuantum: 1_000_000_000_000_000, MinOrderSize: 1_000_000_000_000_000,
	}

	price := TradablePrice(eth)
	qty := Qty(eth, MinLots(eth))
	if got := eth.Notional(price, qty); got < minAssertableNotional {
		t.Fatalf("TradablePrice %d × min qty %d = notional %d, want at least %d", price, qty, got, minAssertableNotional)
	}
	// price × qty here is ~1e25: far past uint64, and exactly what a bare multiply gets wrong.
	if got, want := eth.Notional(1_000_000_000, 10_000_000_000_000_000), uint64(10_000_000); got != want {
		t.Fatalf("Notional(1e9, 1e16) = %d, want %d", got, want)
	}
}
