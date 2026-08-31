package assert

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

func ptr(s string) *string { return &s }

func TestFeesBySymbol(t *testing.T) {
	// One match: buyer paid 3 base fee, seller paid 500 quote fee.
	buyer := client.Order{
		ID:      "buy-1",
		Side:    ptr("buy"),
		Matches: []client.Match{{ID: "m1", Fee: 3}},
	}
	seller := client.Order{
		ID:      "sell-1",
		Side:    ptr("sell"),
		Matches: []client.Match{{ID: "m1", Fee: 500}},
	}

	fees := feesBySymbol("ETH", "USDT", []client.Order{buyer, seller})
	if fees["ETH"] != 3 || fees["USDT"] != 500 {
		t.Fatalf("fees = %v, want ETH:3 USDT:500", fees)
	}

	// Passing the buyer twice must not double-count its fee.
	fees = feesBySymbol("ETH", "USDT", []client.Order{buyer, buyer, seller})
	if fees["ETH"] != 3 {
		t.Fatalf("fees[ETH] = %d after duplicate buyer, want 3", fees["ETH"])
	}
}

func TestConserved(t *testing.T) {
	base, quote := "ETH", "USDT"

	// Buyer: -1000 quote, +10 base minus a 1-base fee. Seller mirrors, minus a 5-quote fee.
	buyerDiff := map[string]Move{
		quote: {Balance: -1000},
		base:  {Balance: 9},
	}
	sellerDiff := map[string]Move{
		base:  {Balance: -10},
		quote: {Balance: 995},
	}
	orders := []client.Order{
		{ID: "b", Side: ptr("buy"), Matches: []client.Match{{ID: "m1", Fee: 1}}},
		{ID: "s", Side: ptr("sell"), Matches: []client.Match{{ID: "m1", Fee: 5}}},
	}

	Conserved(t, base, quote, []map[string]Move{buyerDiff, sellerDiff}, orders...)
}
