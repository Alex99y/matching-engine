package orderbook

import (
	"math"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
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

// Fees are charged on the asset each party receives, at the taker rate for the taker
// and the maker rate for the resting maker, and deducted from the credited amount.
func TestTakerMakerFees(t *testing.T) {
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID:                1,
		BaseInstrumentID:  baseInstr,
		QuoteInstrumentID: quoteInstr,
		TakerFeeBps:       10, // 0.10%
		MakerFeeBps:       5,  // 0.05%
		BaseScale:         1, // decimals=0, matches the quoteAmt comment below
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

// ExpireDue must return only orders whose TTL has elapsed, leaving a not-yet-due order (or
// one with no TTL at all) untouched — it is a prefix scan of the due orders, not a filter
// over the whole book.
func TestExpireDue_OnlyReturnsDueOrders(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	due := restSellExpiring(o, seller, 100, 10, unixPtr(1000))
	notYetDue := restSellExpiring(o, seller, 101, 10, unixPtr(2000))
	noExpiry := restSell(o, seller, 102, 10)

	got := o.ExpireDue(1000)
	if len(got) != 1 || got[0] != due {
		t.Fatalf("ExpireDue(1000) = %v, want just [%s]", got, due)
	}

	got = o.ExpireDue(2000)
	want := map[uuid.UUID]bool{due: true, notYetDue: true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Fatalf("ExpireDue(2000) = %v, want both TTL orders", got)
	}
	for _, id := range got {
		if id == noExpiry {
			t.Fatalf("ExpireDue returned an order with no TTL: %s", noExpiry)
		}
	}
}

// ExpireOrder must have the same book/fund-release effects as CancelOrder — remove the resting
// order, release its blocked reservation, and record the cancellation — while the stream (not
// the DB status) reports "expired" instead of "cancelled" so a subscriber can tell TTL reaps
// apart from user cancels.
func TestExpireOrder_RemovesRestingOrderAndReleasesFunds(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	id := restSellExpiring(o, seller, 100, 10, unixPtr(1000))

	r := repository.NewBatchResult()
	o.ExpireOrder(id, r)

	if s := o.Stats(); s.AskOrders != 0 {
		t.Fatalf("book still has %d resting ask(s) after expiry", s.AskOrders)
	}
	if len(o.ExpireDue(math.MaxInt64)) != 0 {
		t.Fatalf("expired order still present in the expiry index")
	}

	sb := delta(t, r, seller, baseInstr)
	if sb.BalanceDelta != 10 || sb.BlockedDelta != -10 {
		t.Fatalf("seller base: balance=%d blocked=%d (want 10, -10)", sb.BalanceDelta, sb.BlockedDelta)
	}
	if len(r.ClosedOpenOrders) != 1 || r.ClosedOpenOrders[0] != id {
		t.Fatalf("ClosedOpenOrders = %v, want [%s]", r.ClosedOpenOrders, id)
	}
	if len(r.StatusUpdates) != 1 || r.StatusUpdates[0].Status != repository.OrderStatusCancelled {
		t.Fatalf("StatusUpdates = %+v, want one cancelled (no DB status for expired)", r.StatusUpdates)
	}
	assertConserved(t, r)

	upd := findOrderUpdate(o.DrainStream(), id)
	if upd == nil || upd.Status != statusExpired {
		t.Fatalf("stream status = %+v, want %q", upd, statusExpired)
	}
}

// A miss (already filled/cancelled/never existed) is a normal race, mirroring CancelOrder: it
// must be a silent no-op, never touching the result.
func TestExpireOrder_UnknownOrderIsNoop(t *testing.T) {
	o := testBook()
	r := repository.NewBatchResult()
	o.ExpireOrder(uuid.New(), r)

	if len(r.ClosedOpenOrders) != 0 || len(r.StatusUpdates) != 0 || len(r.BalanceDeltas()) != 0 {
		t.Fatalf("ExpireOrder on an unknown id mutated the result: %+v", r)
	}
}

// A partial fill before expiry keeps its true DB status (partially_filled — some quantity
// really did trade), but the stream still reports the TTL reason for why it left the book.
func TestExpireOrder_PartiallyFilledKeepsDBStatusButStreamSaysExpired(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	id := restSellExpiring(o, seller, 100, 10, unixPtr(1000))

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.ImmediateOrCancel,
		Price:       100,
		Quantity:    4,
	}, r)
	o.DrainStream() // discard the fill's own stream events, only the expiry matters below

	o.ExpireOrder(id, r)

	if len(r.StatusUpdates) != 1 || r.StatusUpdates[0].Status != repository.OrderStatusPartiallyFilled {
		t.Fatalf("DB status = %+v, want partially_filled", r.StatusUpdates)
	}
	upd := findOrderUpdate(o.DrainStream(), id)
	if upd == nil || upd.Status != statusExpired {
		t.Fatalf("stream status = %+v, want %q", upd, statusExpired)
	}
	if upd.Filled != 4 || upd.Remaining != 6 {
		t.Fatalf("stream filled/remaining = %d/%d, want 4/6", upd.Filled, upd.Remaining)
	}
}

// A maker fully consumed by a fill must drop out of the expiry index too — otherwise a filled
// order's id would wrongly resurface from a later ExpireDue sweep.
func TestFullyFilledOrder_LeavesExpiryIndex(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	buyer := uuid.New()
	restSellExpiring(o, seller, 100, 10, unixPtr(1000))

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.GoodTillCancel,
		Price:       100,
		Quantity:    10,
	}, r)

	if got := o.ExpireDue(math.MaxInt64); len(got) != 0 {
		t.Fatalf("ExpireDue still reports a fully filled order: %v", got)
	}
}

// CancelOrder must keep unindexing the expiry entry (a cancelled order must not resurface from
// ExpireDue) while continuing to report its plain DB status on the stream, not "expired" — the
// refactor sharing removeResting/closeResting between CancelOrder and ExpireOrder must not blur
// that distinction.
func TestCancelOrder_UnindexesExpiryAndKeepsPlainStatus(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	id := restSellExpiring(o, seller, 100, 10, unixPtr(1000))

	r := repository.NewBatchResult()
	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: id}, r)

	if got := o.ExpireDue(math.MaxInt64); len(got) != 0 {
		t.Fatalf("cancelled order still present in the expiry index: %v", got)
	}
	upd := findOrderUpdate(o.DrainStream(), id)
	if upd == nil || upd.Status != repository.OrderStatusCancelled {
		t.Fatalf("stream status = %+v, want plain %q", upd, repository.OrderStatusCancelled)
	}
}

// Hydrate must repopulate the expiry index from persisted orders (not just the price-level
// book), so a TTL set before a restart is still enforced after one.
func TestHydrate_RepopulatesExpiryIndex(t *testing.T) {
	o := testBook()
	id := restSellExpiring(o, uuid.New(), 100, 10, unixPtr(1000))

	if got := o.ExpireDue(1000); len(got) != 1 || got[0] != id {
		t.Fatalf("ExpireDue after Hydrate = %v, want [%s]", got, id)
	}
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
