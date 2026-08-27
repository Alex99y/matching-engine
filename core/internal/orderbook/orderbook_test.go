package orderbook

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file holds the fixtures shared by every _test.go in this package: a bare book, a
// resting-sell helper, and the balance/conservation/stream assertions the other test files
// build on. No Test functions live here.

const (
	baseInstr  = 10
	quoteInstr = 20
)

func testBook() *OrderBook {
	return NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID:                1,
		BaseInstrumentID:  baseInstr,
		QuoteInstrumentID: quoteInstr,
		BaseScale:         1, // decimals=0: every quantity below is unscaled, matches quoteAmt comments
	})
}

func restSell(o *OrderBook, user uuid.UUID, price, base uint64) uuid.UUID {
	return restSellExpiring(o, user, price, base, nil)
}

func restSellExpiring(o *OrderBook, user uuid.UUID, price, base uint64, expiresAt *int64) uuid.UUID {
	id := uuid.New()
	o.Hydrate([]repository.OpenOrderHydration{{
		OrderID:             id,
		UserID:              user,
		Side:                "sell",
		Price:               price,
		Type:                "limit",
		TimeInForce:         "GTC",
		RemainingHaveAmount: base,         // sell: have = base
		RemainingWantAmount: price * base, // sell: want = quote
		ExpiresAt:           expiresAt,
	}})
	return id
}

func restBuy(o *OrderBook, user uuid.UUID, price, base uint64) uuid.UUID {
	id := uuid.New()
	o.Hydrate([]repository.OpenOrderHydration{{
		OrderID:             id,
		UserID:              user,
		Side:                "buy",
		Price:               price,
		Type:                "limit",
		TimeInForce:         "GTC",
		RemainingHaveAmount: price * base, // buy: have = quote
		RemainingWantAmount: base,         // buy: want = base
	}})
	return id
}

func unixPtr(t int64) *int64 { return &t }

func delta(t *testing.T, r *repository.BatchResult, user uuid.UUID, instr int) repository.BalanceDelta {
	t.Helper()
	for _, d := range r.BalanceDeltas() {
		if d.UserID == user && d.InstrumentID == instr {
			return d
		}
	}
	return repository.BalanceDelta{UserID: user, InstrumentID: instr}
}

// assertConserved checks that, summed across all users, each instrument's total
// (balance + blocked) movement equals the negative of the fees collected in that
// instrument — funds are only ever transferred, minus what the house takes as fees.
func assertConserved(t *testing.T, r *repository.BatchResult) {
	t.Helper()
	net := map[int]int64{}
	for _, d := range r.BalanceDeltas() {
		net[d.InstrumentID] += d.BalanceDelta + d.BlockedDelta
	}
	fees := map[int]int64{}
	for _, m := range r.Matches {
		fees[baseInstr] += int64(m.MatchBuyFees)   // buyer fee, in base
		fees[quoteInstr] += int64(m.MatchSellFees) // seller fee, in quote
	}
	for instr, n := range net {
		if n != -fees[instr] {
			t.Fatalf("instrument %d not conserved: net %d, fees %d (want net == -fees)", instr, n, fees[instr])
		}
	}
}

// findOrderUpdate returns the last stream order-update recorded for id, or nil if none was.
func findOrderUpdate(snap StreamSnapshot, id uuid.UUID) *marketdata.OrderUpdate {
	var found *marketdata.OrderUpdate
	for i := range snap.Orders {
		if snap.Orders[i].Update.OrderID == id.String() {
			u := snap.Orders[i].Update
			found = &u
		}
	}
	return found
}
