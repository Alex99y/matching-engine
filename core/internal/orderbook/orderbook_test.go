package orderbook

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

const (
	baseInstr  = 10
	quoteInstr = 20
)

func testBook() *OrderBook {
	return NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID:                1,
		BaseInstrumentID:  baseInstr,
		QuoteInstrumentID: quoteInstr,
	})
}

func restSell(o *OrderBook, user uuid.UUID, price, base uint64) uuid.UUID {
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
	}})
	return id
}

func delta(t *testing.T, r *repository.BatchResult, user uuid.UUID, instr int) repository.BalanceDelta {
	t.Helper()
	for _, d := range r.BalanceDeltas() {
		if d.UserID == user && d.InstrumentID == instr {
			return d
		}
	}
	return repository.BalanceDelta{UserID: user, InstrumentID: instr}
}

// A limit buy crossing a cheaper resting sell must trade at the maker's price and
// release the buyer's over-reservation (it reserved at its own higher limit).
func TestPriceImprovementRelease(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	restSell(o, seller, 100, 10)

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.GoodTillCancel,
		Price:       120, // willing to pay up to 120, reserve = 1200
		Quantity:    10,
	}, r)

	if len(r.Matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(r.Matches))
	}
	m := r.Matches[0]
	if m.MatchPrice != 100 || m.MatchBuyAmount != 10 || m.MatchSellAmount != 1000 {
		t.Fatalf("match: price=%d buy=%d sell=%d", m.MatchPrice, m.MatchBuyAmount, m.MatchSellAmount)
	}
	if !m.IsBuyOrderFilled || !m.IsSellOrderFilled || !m.BuyOrderIsTaker {
		t.Fatalf("match flags: buyFilled=%v sellFilled=%v buyTaker=%v", m.IsBuyOrderFilled, m.IsSellOrderFilled, m.BuyOrderIsTaker)
	}

	// Buyer: reserved 1200 quote. Spends 1000, releases 200 of price improvement.
	bq := delta(t, r, buyer, quoteInstr)
	if bq.BlockedDelta != -1200 || bq.BalanceDelta != 200 {
		t.Fatalf("buyer quote: blocked=%d balance=%d (want -1200, 200)", bq.BlockedDelta, bq.BalanceDelta)
	}
	bb := delta(t, r, buyer, baseInstr)
	if bb.BalanceDelta != 10 || bb.BlockedDelta != 0 {
		t.Fatalf("buyer base: balance=%d blocked=%d (want 10, 0)", bb.BalanceDelta, bb.BlockedDelta)
	}
	// Seller: 10 base leaves blocked, 1000 quote received.
	sb := delta(t, r, seller, baseInstr)
	if sb.BlockedDelta != -10 || sb.BalanceDelta != 0 {
		t.Fatalf("seller base: blocked=%d balance=%d (want -10, 0)", sb.BlockedDelta, sb.BalanceDelta)
	}
	sq := delta(t, r, seller, quoteInstr)
	if sq.BalanceDelta != 1000 {
		t.Fatalf("seller quote balance=%d (want 1000)", sq.BalanceDelta)
	}

	// Conservation: total (balance+blocked) moved per instrument must net to zero.
	assertConserved(t, r)

	if got := r.NewOrders[0].Status; got != repository.OrderStatusFilled {
		t.Fatalf("taker status=%q want filled", got)
	}
}

// A partial fill that rests keeps exactly the reservation backing the remainder and
// releases only the improvement on the filled portion.
func TestPartialFillRests(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	restSell(o, seller, 100, 4)

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.GoodTillCancel,
		Price:       120,
		Quantity:    10,
	}, r)

	if len(r.Matches) != 1 || r.Matches[0].MatchBuyAmount != 4 {
		t.Fatalf("want 1 match of 4, got %+v", r.Matches)
	}
	if len(r.OpenOrders) != 1 {
		t.Fatalf("want taker resting, got %d open orders", len(r.OpenOrders))
	}
	oo := r.OpenOrders[0]
	if oo.RemainingHaveAmount != 720 || oo.RemainingWantAmount != 6 { // 120*6 quote, 6 base
		t.Fatalf("resting remainder have=%d want=%d (want 720, 6)", oo.RemainingHaveAmount, oo.RemainingWantAmount)
	}
	// Buyer spent 400, reserved 1200, keeps 720 blocked for the rest → releases 80.
	bq := delta(t, r, buyer, quoteInstr)
	if bq.BlockedDelta != -480 || bq.BalanceDelta != 80 {
		t.Fatalf("buyer quote: blocked=%d balance=%d (want -480, 80)", bq.BlockedDelta, bq.BalanceDelta)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusOpen {
		t.Fatalf("taker status=%q want open", got)
	}
	assertConserved(t, r)
}

// A market buy is a quote budget: it walks asks spending until the budget is gone,
// trading at maker prices, and releases any unspendable remainder.
func TestMarketBuyQuoteBudget(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	restSell(o, seller, 100, 3) // 3 base available at 100

	budget := uint64(1000)
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.MarketOrder,
		TimeInForce: oeq.ImmediateOrCancel,
		QuoteQty:    &budget,
	}, r)

	// Affords 3 base (300 quote), 700 unspendable → released.
	if len(r.Matches) != 1 || r.Matches[0].MatchBuyAmount != 3 {
		t.Fatalf("want 1 match of 3, got %+v", r.Matches)
	}
	bq := delta(t, r, buyer, quoteInstr)
	if bq.BlockedDelta != -1000 || bq.BalanceDelta != 700 {
		t.Fatalf("buyer quote: blocked=%d balance=%d (want -1000, 700)", bq.BlockedDelta, bq.BalanceDelta)
	}
	assertConserved(t, r)
}

// assertConserved checks that, summed across all users, each instrument's total
// (balance + blocked) movement nets to zero — funds are only ever transferred.
func assertConserved(t *testing.T, r *repository.BatchResult) {
	t.Helper()
	net := map[int]int64{}
	for _, d := range r.BalanceDeltas() {
		net[d.InstrumentID] += d.BalanceDelta + d.BlockedDelta
	}
	for instr, n := range net {
		if n != 0 {
			t.Fatalf("instrument %d not conserved: net %d", instr, n)
		}
	}
}
