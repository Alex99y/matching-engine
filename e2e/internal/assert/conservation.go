package assert

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// Conserved asserts fund conservation across a completed trade: summed over the supplied
// balance diffs, each of the market's two symbols moves by exactly the negative of the fees
// taken in that symbol, and no other symbol moves at all.
//
// Fees are read from the participating orders' matches (each order fetched via GetOrder, so
// Matches is populated). Pass every order that took part — for a two-party trade, both the
// taker and the maker: the buyer's fee is charged in the base symbol, the seller's in quote.
func Conserved(t testing.TB, baseSymbol, quoteSymbol string, diffs []map[string]Move, orders ...client.Order) {
	t.Helper()

	for _, o := range orders {
		if o.Side == nil {
			t.Fatalf("assert.Conserved: order %s has no side — cannot attribute its fees", o.ID)
		}
	}

	fees := feesBySymbol(baseSymbol, quoteSymbol, orders)
	net := netBySymbol(diffs)

	for _, sym := range []string{baseSymbol, quoteSymbol} {
		if want := -fees[sym]; net[sym] != want {
			t.Fatalf("assert.Conserved: %s net movement = %d, want %d (= -fees)", sym, net[sym], want)
		}
	}
	for sym, n := range net {
		if sym != baseSymbol && sym != quoteSymbol && n != 0 {
			t.Fatalf("assert.Conserved: unexpected movement in %s: %d", sym, n)
		}
	}
}

// feesBySymbol totals the fees the given orders paid, per symbol. A buy order's per-match fee
// is in base, a sell order's in quote. The (matchID, symbol) dedupe guards against the caller
// passing the same order twice; it never collapses a real buyer+seller pair, whose fees land
// in different symbols.
func feesBySymbol(baseSymbol, quoteSymbol string, orders []client.Order) map[string]int64 {
	fees := map[string]int64{}
	seen := map[string]struct{}{}

	for _, o := range orders {
		sym := quoteSymbol
		if o.Side != nil && *o.Side == string(client.Buy) {
			sym = baseSymbol
		}
		for _, m := range o.Matches {
			key := m.ID + "|" + sym
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			fees[sym] += int64(m.Fee)
		}
	}
	return fees
}
