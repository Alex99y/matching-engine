package orderbook

import (
	"math"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

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

// price is quote-quanta per whole base coin, so on a market with baseDecimals > 0 a market
// buy whose budget is smaller than one whole coin's price must still trade — the affordable
// base quantity is budget * baseScale / price. Regression for canTrade/fillQty treating
// price as quote-per-quantum, which cancelled every such order with zero fills.
func TestMarketBuyBudgetBelowUnitPriceStillFills(t *testing.T) {
	const baseScale = 1_000
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: baseScale,
	})
	seller := uuid.New()
	buyer := uuid.New()
	restSell(o, seller, 2000, 5000) // price 2000/coin, 5000 base-quanta resting

	budget := uint64(500) // less than price (2000) but buys 500*1000/2000 = 250 base-quanta
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: buyer, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.ImmediateOrCancel,
		QuoteQty: &budget,
	}, r)

	if len(r.Matches) != 1 || r.Matches[0].MatchBuyAmount != 250 {
		t.Fatalf("want 1 match of 250 base, got %+v", r.Matches)
	}
	if r.Matches[0].MatchSellAmount != 500 {
		t.Fatalf("quote spent = %d, want 500", r.Matches[0].MatchSellAmount)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusFilled {
		t.Fatalf("taker status = %q, want filled (budget fully spent)", got)
	}
	if bq := delta(t, r, buyer, quoteInstr); bq.BlockedDelta != -500 || bq.BalanceDelta != 0 {
		t.Fatalf("buyer quote: blocked=%d balance=%d (want -500, 0)", bq.BlockedDelta, bq.BalanceDelta)
	}
	if bb := delta(t, r, buyer, baseInstr); bb.BalanceDelta != 250 {
		t.Fatalf("buyer base credit = %d, want 250", bb.BalanceDelta)
	}
	assertConserved(t, r)
}

// A market buy almost never spends its budget to zero (integer division on both the
// affordable quantity and the fill cost). When the leftover cannot buy a single base quantum
// at the price it just traded at, the order is filled — the dust is refunded, not recorded
// as a cancellation — and that holds whether or not any liquidity remains behind it.
func TestMarketBuyDustRemainderCountsAsFilled(t *testing.T) {
	const baseScale = 1_000
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: baseScale,
	})
	buyer := uuid.New()
	restSell(o, uuid.New(), 2000, 5000)

	budget := uint64(501) // buys 501*1000/2000 = 250 base for 500 quote; 1 quantum dust left
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: buyer, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.ImmediateOrCancel,
		QuoteQty: &budget,
	}, r)

	if len(r.Matches) != 1 || r.Matches[0].MatchBuyAmount != 250 {
		t.Fatalf("want 1 match of 250 base, got %+v", r.Matches)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusFilled {
		t.Fatalf("taker status = %q, want filled", got)
	}
	if len(r.CancelledOrders) != 0 {
		t.Fatalf("dust remainder recorded a cancellation: %+v", r.CancelledOrders)
	}
	// 500 spent, 1 dust refunded from the 501 reserved.
	if bq := delta(t, r, buyer, quoteInstr); bq.BlockedDelta != -501 || bq.BalanceDelta != 1 {
		t.Fatalf("buyer quote: blocked=%d balance=%d (want -501, 1)", bq.BlockedDelta, bq.BalanceDelta)
	}
	assertConserved(t, r)
}

// The dust verdict must not depend on what is left on the book: consuming the only resting
// ask empties that side entirely, and the order is still filled. Guards the earlier rule,
// which asked whether the opposite side had levels remaining and so reported this order —
// which got every unit it could afford — as partially filled.
func TestMarketBuyDustCountsAsFilledEvenWhenItEmptiesTheBook(t *testing.T) {
	const baseScale = 1_000
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: baseScale,
	})
	buyer := uuid.New()
	restSell(o, uuid.New(), 2000, 250) // the only ask, and the buy consumes all of it

	budget := uint64(501) // buys the full 250 base for 500; 1 quantum of dust left over
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: buyer, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.ImmediateOrCancel,
		QuoteQty: &budget,
	}, r)

	if s := o.Stats(); s.AskOrders != 0 {
		t.Fatalf("the ask side still holds %d order(s); this test needs it emptied", s.AskOrders)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusFilled {
		t.Fatalf("taker status = %q, want filled — it bought every unit it could afford", got)
	}
	if len(r.CancelledOrders) != 0 {
		t.Fatalf("dust was recorded as a cancellation: %+v", r.CancelledOrders)
	}
	assertConserved(t, r)
}

// When the book genuinely runs dry with real budget left, the market buy is partially
// filled and the unspent budget is recorded as a cancelled remainder.
func TestMarketBuyPartialWhenBookRunsDry(t *testing.T) {
	const baseScale = 1_000
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: baseScale,
	})
	buyer := uuid.New()
	restSell(o, uuid.New(), 2000, 100) // only 100 base for sale, worth 200 quote

	budget := uint64(1000)
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: buyer, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.ImmediateOrCancel,
		QuoteQty: &budget,
	}, r)

	if len(r.Matches) != 1 || r.Matches[0].MatchBuyAmount != 100 {
		t.Fatalf("want 1 match of 100 base, got %+v", r.Matches)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusPartiallyFilled {
		t.Fatalf("taker status = %q, want partially_filled", got)
	}
	if len(r.CancelledOrders) != 1 || r.CancelledOrders[0].RemainingHaveAmount != 800 {
		t.Fatalf("unspent budget not recorded: %+v", r.CancelledOrders)
	}
	assertConserved(t, r)
}

// canFill (the FOK pre-check) must value each ask level as price*qty/baseScale too — a
// market-buy FOK that the book cannot fully absorb is killed untouched, and one it can is
// filled.
func TestMarketBuyFillOrKillRespectsBaseScale(t *testing.T) {
	const baseScale = 1_000
	newBook := func() *OrderBook {
		o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
			ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: baseScale,
		})
		restSell(o, uuid.New(), 2000, 100) // level worth 2000*100/1000 = 200 quote
		return o
	}

	tooBig := uint64(500) // > 200 available → cannot fully fill
	r := repository.NewBatchResult()
	newBook().MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.FillOrKill,
		QuoteQty: &tooBig,
	}, r)
	if len(r.Matches) != 0 {
		t.Fatalf("FOK that cannot fully fill traded: %+v", r.Matches)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusCancelled {
		t.Fatalf("killed FOK status = %q, want cancelled", got)
	}

	ok := uint64(150) // buys 75 base for 150 quote, within the 200 available
	r2 := repository.NewBatchResult()
	newBook().MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.FillOrKill,
		QuoteQty: &ok,
	}, r2)
	if len(r2.Matches) != 1 || r2.Matches[0].MatchBuyAmount != 75 {
		t.Fatalf("want 1 match of 75 base, got %+v", r2.Matches)
	}
	if got := r2.NewOrders[0].Status; got != repository.OrderStatusFilled {
		t.Fatalf("filled FOK status = %q, want filled", got)
	}
}

// Fees are charged on the asset each party receives, at the taker rate for the taker
// and the maker rate for the resting maker, and deducted from the credited amount.
func TestTakerMakerFees(t *testing.T) {
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID:                1,
		BaseInstrumentID:  baseInstr,
		QuoteInstrumentID: quoteInstr,
		TakerFeeBps:       10, // 0.10%
		MakerFeeBps:       5,  // 0.05%
		BaseScale:         1,  // decimals=0, matches the quoteAmt comment below
	})
	seller := uuid.New() // resting maker (sell)
	buyer := uuid.New()  // incoming taker (buy)
	restSell(o, seller, 100, 10000)

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.GoodTillCancel,
		Price:       100,
		Quantity:    10000,
	}, r)

	// Trade: 10000 base @ 100 → quoteAmt 1,000,000.
	// Buyer is taker → base fee = 10000 * 10 / 10000 = 10.
	// Seller is maker → quote fee = 1,000,000 * 5 / 10000 = 500.
	m := r.Matches[0]
	if m.MatchBuyFees != 10 || m.MatchSellFees != 500 {
		t.Fatalf("fees: buy=%d sell=%d (want 10, 500)", m.MatchBuyFees, m.MatchSellFees)
	}
	if bb := delta(t, r, buyer, baseInstr); bb.BalanceDelta != 10000-10 {
		t.Fatalf("buyer base credit=%d (want 9990)", bb.BalanceDelta)
	}
	if sq := delta(t, r, seller, quoteInstr); sq.BalanceDelta != 1000000-500 {
		t.Fatalf("seller quote credit=%d (want 999500)", sq.BalanceDelta)
	}
	assertConserved(t, r) // now holds with net == -fees
}

// feeOf must never panic, even if bps somehow exceeds the DB CHECK's 10000 cap (a raw DB edit or
// a bad migration bypasses that constraint) — bits.Div64 panics once the quotient overflows 64
// bits, which happens for any bps >= 10000. It should clamp to a 100% fee instead of crashing the
// matcher goroutine (and, with it, every market sharing the process).
func TestFeeOfClampsOutOfRangeBps(t *testing.T) {
	cases := []struct {
		name        string
		amount, bps uint64
	}{
		{"bps double the cap", 1_000_000, 20_000},
		{"bps at uint64 max", 1_000_000, math.MaxUint64},
		{"amount at uint64 max, bps over cap", math.MaxUint64, 20_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := feeOf(c.amount, c.bps); got != c.amount {
				t.Fatalf("feeOf(%d, %d) = %d, want %d (clamped to 100%%)", c.amount, c.bps, got, c.amount)
			}
		})
	}
}

func postOnlyBuy(id, user uuid.UUID, price, qty uint64) *oeq.OpenOrderEvent {
	return &oeq.OpenOrderEvent{
		OrderID: id, UserID: user, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: price, Quantity: qty, PostOnly: true,
	}
}

// A post-only buy priced below the best ask does not cross, so it rests like any GTC limit
// and releases nothing.
func TestPostOnlyRestsWhenItDoesNotCross(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	restSell(o, uuid.New(), 100, 10)

	id := uuid.New()
	r := repository.NewBatchResult()
	o.MatchOrder(postOnlyBuy(id, buyer, 95, 4), r)

	if len(r.Matches) != 0 {
		t.Fatalf("post-only order traded: %+v", r.Matches)
	}
	if len(r.OpenOrders) != 1 || r.OpenOrders[0].OrderID != id {
		t.Fatalf("post-only order did not rest: %+v", r.OpenOrders)
	}
	if len(r.CancelledOrders) != 0 {
		t.Fatalf("resting post-only order recorded a cancellation: %+v", r.CancelledOrders)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusOpen {
		t.Fatalf("taker status=%q want open", got)
	}
	if bq := delta(t, r, buyer, quoteInstr); bq.BlockedDelta != 0 || bq.BalanceDelta != 0 {
		t.Fatalf("buyer quote: balance=%d blocked=%d (want 0, 0 — nothing released)", bq.BalanceDelta, bq.BlockedDelta)
	}
	if s := o.Stats(); s.BidOrders != 1 {
		t.Fatalf("book has %d resting bids, want 1", s.BidOrders)
	}
	if upd := findOrderUpdate(o.DrainStream(), id); upd == nil || upd.Status != repository.OrderStatusOpen {
		t.Fatalf("stream status = %+v, want open", upd)
	}
}

// A post-only buy that crosses the best ask is cancelled untouched: no fill, nothing rests,
// the full reservation is released.
func TestPostOnlyRejectedWhenItWouldCross(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	restSell(o, uuid.New(), 100, 10)

	id := uuid.New()
	r := repository.NewBatchResult()
	o.MatchOrder(postOnlyBuy(id, buyer, 105, 4), r)

	if len(r.Matches) != 0 {
		t.Fatalf("post-only order traded: %+v", r.Matches)
	}
	if len(r.OpenOrders) != 0 {
		t.Fatalf("rejected post-only order rested: %+v", r.OpenOrders)
	}
	if len(r.CancelledOrders) != 1 || r.CancelledOrders[0].OrderID != id {
		t.Fatalf("CancelledOrders = %+v, want one for %s", r.CancelledOrders, id)
	}
	if cr := r.CancelledOrders[0]; cr.RemainingHaveAmount != 420 || cr.RemainingWantAmount != 4 {
		t.Fatalf("cancelled remainder have=%d want=%d (want 420, 4)", cr.RemainingHaveAmount, cr.RemainingWantAmount)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusCancelled {
		t.Fatalf("taker DB status=%q want cancelled", got)
	}
	if bq := delta(t, r, buyer, quoteInstr); bq.BalanceDelta != 420 || bq.BlockedDelta != -420 {
		t.Fatalf("buyer quote: balance=%d blocked=%d (want 420, -420 — full release)", bq.BalanceDelta, bq.BlockedDelta)
	}
	if s := o.Stats(); s.BidOrders != 0 || s.AskOrders != 1 {
		t.Fatalf("book changed: bids=%d asks=%d (want 0, 1)", s.BidOrders, s.AskOrders)
	}
	if upd := findOrderUpdate(o.DrainStream(), id); upd == nil || upd.Status != repository.OrderStatusCancelled {
		t.Fatalf("stream status = %+v, want cancelled", upd)
	}
}

// A limit priced exactly at the best opposite level is marketable (crosses uses >=/<=), so a
// post-only order at the touch is rejected, not rested.
func TestPostOnlyRejectedAtExactTouchPrice(t *testing.T) {
	o := testBook()
	restSell(o, uuid.New(), 100, 10)

	r := repository.NewBatchResult()
	o.MatchOrder(postOnlyBuy(uuid.New(), uuid.New(), 100, 4), r)

	if len(r.OpenOrders) != 0 || len(r.CancelledOrders) != 1 {
		t.Fatalf("touch-priced post-only not rejected: open=%d cancelled=%d", len(r.OpenOrders), len(r.CancelledOrders))
	}
}

// With no liquidity on the opposite side nothing can be crossed, so a post-only order rests.
func TestPostOnlyRestsAgainstEmptyOppositeBook(t *testing.T) {
	o := testBook()

	r := repository.NewBatchResult()
	o.MatchOrder(postOnlyBuy(uuid.New(), uuid.New(), 100, 4), r)

	if len(r.OpenOrders) != 1 || len(r.CancelledOrders) != 0 {
		t.Fatalf("post-only order against empty book not rested: open=%d cancelled=%d", len(r.OpenOrders), len(r.CancelledOrders))
	}
}

// The reject check is side-aware: a post-only sell that undercuts the best bid crosses and is
// rejected, releasing its base reservation.
func TestPostOnlySellRejectedWhenItWouldCross(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	restBuy(o, uuid.New(), 100, 10)

	id := uuid.New()
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: id, UserID: seller, MarketID: 1,
		Side: oeq.SellOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 95, Quantity: 4, PostOnly: true,
	}, r)

	if len(r.Matches) != 0 || len(r.OpenOrders) != 0 || len(r.CancelledOrders) != 1 {
		t.Fatalf("post-only sell not rejected: matches=%d open=%d cancelled=%d",
			len(r.Matches), len(r.OpenOrders), len(r.CancelledOrders))
	}
	if sb := delta(t, r, seller, baseInstr); sb.BalanceDelta != 4 || sb.BlockedDelta != -4 {
		t.Fatalf("seller base: balance=%d blocked=%d (want 4, -4)", sb.BalanceDelta, sb.BlockedDelta)
	}
	if upd := findOrderUpdate(o.DrainStream(), id); upd == nil || upd.Status != repository.OrderStatusCancelled {
		t.Fatalf("stream status = %+v, want cancelled", upd)
	}
}

// An order failing the defensive guards must be cancelled, never rested — a limit GTC with a
// zero price satisfies takerRests on its own, so it would otherwise sit in the book at price 0.
func TestGuardFailureIsCancelledNotRested(t *testing.T) {
	o := testBook()
	id := uuid.New()
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: id, UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 0, Quantity: 10,
	}, r)

	if s := o.Stats(); s.BidOrders != 0 {
		t.Fatalf("guard-failed order rested in the book: bidOrders=%d", s.BidOrders)
	}
	if len(r.OpenOrders) != 0 || len(r.CancelledOrders) != 1 {
		t.Fatalf("open=%d cancelled=%d (want 0, 1)", len(r.OpenOrders), len(r.CancelledOrders))
	}
	if r.NewOrders[0].Status != repository.OrderStatusCancelled {
		t.Fatalf("status = %s, want cancelled", r.NewOrders[0].Status)
	}
}

// The mirror of TestPriceImprovementRelease: a sell taker crossing a higher resting bid trades
// at the maker's better price. Exercises emitTrade's buyer/seller swap, which no other test in
// the package reaches — every other taker here is a buy.
func TestSellTakerPriceImprovement(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	restBuy(o, buyer, 120, 10)

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: seller, MarketID: 1,
		Side: oeq.SellOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10, // asks 100 or better; the resting bid pays 120
	}, r)

	if len(r.Matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(r.Matches))
	}
	m := r.Matches[0]
	if m.MatchPrice != 120 || m.MatchBuyAmount != 10 || m.MatchSellAmount != 1200 {
		t.Fatalf("match: price=%d buy=%d sell=%d (want 120, 10, 1200)", m.MatchPrice, m.MatchBuyAmount, m.MatchSellAmount)
	}
	if m.BuyOrderIsTaker {
		t.Fatalf("BuyOrderIsTaker = true, want false — the sell is the taker here")
	}
	if sb := delta(t, r, seller, baseInstr); sb.BlockedDelta != -10 || sb.BalanceDelta != 0 {
		t.Fatalf("seller base: blocked=%d balance=%d (want -10, 0)", sb.BlockedDelta, sb.BalanceDelta)
	}
	if sq := delta(t, r, seller, quoteInstr); sq.BalanceDelta != 1200 {
		t.Fatalf("seller quote balance=%d (want 1200, the maker's price)", sq.BalanceDelta)
	}
	if bq := delta(t, r, buyer, quoteInstr); bq.BlockedDelta != -1200 {
		t.Fatalf("buyer quote blocked=%d (want -1200)", bq.BlockedDelta)
	}
	if bb := delta(t, r, buyer, baseInstr); bb.BalanceDelta != 10 {
		t.Fatalf("buyer base credit=%d (want 10)", bb.BalanceDelta)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusFilled {
		t.Fatalf("taker status=%q want filled", got)
	}
	assertConserved(t, r)
}

// The mirror of TestPartialFillRests, and the only test that runs settleTakerCompletion's sell
// path with a non-zero filled amount: a sell taker blocks base, delivers part of it, and must
// keep exactly the remainder blocked when it rests — releasing nothing.
func TestSellTakerPartialFillRests(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	restBuy(o, buyer, 120, 4)

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: seller, MarketID: 1,
		Side: oeq.SellOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10,
	}, r)

	if len(r.Matches) != 1 || r.Matches[0].MatchBuyAmount != 4 {
		t.Fatalf("want 1 match of 4, got %+v", r.Matches)
	}
	if len(r.OpenOrders) != 1 {
		t.Fatalf("want taker resting, got %d open orders", len(r.OpenOrders))
	}
	// sell: have = base remainder, want = quote at its own limit (100 * 6).
	if oo := r.OpenOrders[0]; oo.RemainingHaveAmount != 6 || oo.RemainingWantAmount != 600 {
		t.Fatalf("resting remainder have=%d want=%d (want 6, 600)", oo.RemainingHaveAmount, oo.RemainingWantAmount)
	}
	if sb := delta(t, r, seller, baseInstr); sb.BlockedDelta != -4 || sb.BalanceDelta != 0 {
		t.Fatalf("seller base: blocked=%d balance=%d (want -4, 0 — the resting 6 stays blocked)", sb.BlockedDelta, sb.BalanceDelta)
	}
	if sq := delta(t, r, seller, quoteInstr); sq.BalanceDelta != 480 {
		t.Fatalf("seller quote balance=%d (want 480)", sq.BalanceDelta)
	}
	if got := r.NewOrders[0].Status; got != repository.OrderStatusOpen {
		t.Fatalf("taker status=%q want open", got)
	}
	assertConserved(t, r)
}

// The fee-role mirror of TestTakerMakerFees. The two rates are deliberately different, so a
// swapped role assignment in emitTrade yields different numbers rather than the same ones.
func TestSellTakerFeeRoles(t *testing.T) {
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr,
		TakerFeeBps: 10, MakerFeeBps: 5, BaseScale: 1,
	})
	buyer := uuid.New()  // resting maker (buy)
	seller := uuid.New() // incoming taker (sell)
	restBuy(o, buyer, 100, 10000)

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: seller, MarketID: 1,
		Side: oeq.SellOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10000,
	}, r)

	// 10000 base @ 100 → quoteAmt 1,000,000. The buyer is the MAKER now (5 bps on the base it
	// receives = 5); the seller is the TAKER (10 bps on the quote it receives = 1000). Both
	// numbers are the inverse of TestTakerMakerFees, where the buyer was the taker.
	m := r.Matches[0]
	if m.MatchBuyFees != 5 || m.MatchSellFees != 1000 {
		t.Fatalf("fees: buy=%d sell=%d (want 5, 1000 — roles inverted vs TestTakerMakerFees)", m.MatchBuyFees, m.MatchSellFees)
	}
	if bb := delta(t, r, buyer, baseInstr); bb.BalanceDelta != 10000-5 {
		t.Fatalf("buyer base credit=%d (want 9995)", bb.BalanceDelta)
	}
	if sq := delta(t, r, seller, quoteInstr); sq.BalanceDelta != 1000000-1000 {
		t.Fatalf("seller quote credit=%d (want 999000)", sq.BalanceDelta)
	}
	assertConserved(t, r)
}

// Two makers sharing a price must fill in arrival order. Nothing else in the package rests two
// orders at the same price, so this is also the only coverage of getOrCreate returning an
// existing level instead of creating one.
func TestFIFOTimePriorityWithinLevel(t *testing.T) {
	o := testBook()
	oldest := restSell(o, uuid.New(), 100, 4)
	newest := restSell(o, uuid.New(), 100, 4)

	if s := o.Stats(); s.AskOrders != 2 {
		t.Fatalf("both makers should share one level: askOrders=%d, want 2", s.AskOrders)
	}

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.ImmediateOrCancel,
		Price: 100, Quantity: 4,
	}, r)

	if len(r.Matches) != 1 {
		t.Fatalf("want 1 match, got %+v", r.Matches)
	}
	if got := r.Matches[0].SellOrderID; got != oldest {
		t.Fatalf("filled %s first, want the older maker %s", got, oldest)
	}
	if s := o.Stats(); s.AskOrders != 1 {
		t.Fatalf("askOrders=%d after consuming one of two, want 1", s.AskOrders)
	}
	// The survivor keeps the level alive with only its own quantity.
	if _, asks := o.SnapshotLevels(); len(asks) != 1 || asks[0].Quantity != 4 {
		t.Fatalf("asks=%+v, want the 100 level holding the newest maker's 4 (%s)", asks, newest)
	}
}

// A taker sweeping several levels must empty them and remove them from the tree — the
// collect-then-delete pass that runs after Ascend returns. It must also stop at the first level
// its limit does not reach, leaving that level untouched.
func TestMultiLevelSweepDeletesEmptiedLevels(t *testing.T) {
	o := testBook()
	restSell(o, uuid.New(), 100, 3)
	restSell(o, uuid.New(), 101, 3)
	restSell(o, uuid.New(), 105, 3) // above the taker's limit — must not trade

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.ImmediateOrCancel,
		Price: 101, Quantity: 10,
	}, r)

	if len(r.Matches) != 2 {
		t.Fatalf("want 2 matches (one per swept level), got %d: %+v", len(r.Matches), r.Matches)
	}
	if r.Matches[0].MatchPrice != 100 || r.Matches[1].MatchPrice != 101 {
		t.Fatalf("swept out of price order: %d then %d (want 100 then 101)", r.Matches[0].MatchPrice, r.Matches[1].MatchPrice)
	}

	// Stats counts orders, so an emptied-but-still-present level would be invisible to it.
	// SnapshotLevels lists levels, so it proves the two emptied ones actually left the tree.
	bids, asks := o.SnapshotLevels()
	if len(bids) != 0 {
		t.Fatalf("IOC taker rested: bids=%+v", bids)
	}
	if len(asks) != 1 || asks[0].Price != 105 || asks[0].Quantity != 3 {
		t.Fatalf("asks=%+v, want only the untouched 105 level with qty 3", asks)
	}
}

// The FOK pre-check for a base-denominated taker: it must total only the levels the order
// actually crosses, across as many as it takes. Every other FOK test here is a quote-
// denominated market buy, which takes the other branch of canFill.
func TestLimitFillOrKillAcrossLevels(t *testing.T) {
	newBook := func() *OrderBook {
		o := testBook()
		restSell(o, uuid.New(), 100, 3)
		restSell(o, uuid.New(), 101, 3)
		return o
	}
	fokBuy := func(price, qty uint64) *oeq.OpenOrderEvent {
		return &oeq.OpenOrderEvent{
			OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
			Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.FillOrKill,
			Price: price, Quantity: qty,
		}
	}

	t.Run("fills when the crossed levels together cover it", func(t *testing.T) {
		r := repository.NewBatchResult()
		newBook().MatchOrder(fokBuy(101, 6), r)
		if len(r.Matches) != 2 {
			t.Fatalf("want 2 matches, got %d: %+v", len(r.Matches), r.Matches)
		}
		if got := r.NewOrders[0].Status; got != repository.OrderStatusFilled {
			t.Fatalf("status=%q want filled", got)
		}
	})

	t.Run("killed when the book is one unit short", func(t *testing.T) {
		r := repository.NewBatchResult()
		newBook().MatchOrder(fokBuy(101, 7), r)
		if len(r.Matches) != 0 {
			t.Fatalf("FOK traded despite insufficient liquidity: %+v", r.Matches)
		}
		if got := r.NewOrders[0].Status; got != repository.OrderStatusCancelled {
			t.Fatalf("status=%q want cancelled", got)
		}
	})

	t.Run("counts only levels it crosses, not total book depth", func(t *testing.T) {
		r := repository.NewBatchResult()
		newBook().MatchOrder(fokBuy(100, 6), r) // 6 rest in total, but only 3 at a price it reaches
		if len(r.Matches) != 0 {
			t.Fatalf("FOK counted a level above its own limit: %+v", r.Matches)
		}
	})
}
