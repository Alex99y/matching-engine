package orderbook

import (
	"math"
	"testing"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// maxExpirySweep is an effectively-unlimited cap, for tests asserting *which* orders are due
// rather than how many one batch may retire at a time.
const maxExpirySweep = math.MaxInt

// An order can outlive its own TTL while queued, so the matcher screens it on arrival. Such an
// order must never reach the book and never trade: its whole reservation goes back, it is persisted
// as cancelled (there is no DB status for expired), and the stream reports it as expired.
func TestExpireIncoming_ReleasesTheWholeReserveWithoutMatching(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	restSell(o, seller, 100, 10) // liquidity the expired buy would otherwise have taken

	buyer, id := uuid.New(), uuid.New()
	r := repository.NewBatchResult()
	o.ExpireIncoming(&oeq.OpenOrderEvent{
		OrderID:     id,
		UserID:      buyer,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.GoodTillCancel,
		Price:       120, // crosses the resting ask — it must still not trade
		Quantity:    10,
		ExpiresAt:   unixPtr(1000),
	}, r)

	if len(r.Matches) != 0 {
		t.Fatalf("an expired order traded: %+v", r.Matches)
	}
	if s := o.Stats(); s.BidOrders != 0 || s.AskOrders != 1 {
		t.Fatalf("book = %d bids / %d asks, want 0 / 1 — an expired order must not rest or consume", s.BidOrders, s.AskOrders)
	}

	// Reserve for a buy is quote: price 120 × qty 10 = 1200, all of it returned.
	qb := delta(t, r, buyer, quoteInstr)
	if qb.BalanceDelta != 1200 || qb.BlockedDelta != -1200 {
		t.Fatalf("buyer quote: balance=%d blocked=%d (want 1200, -1200)", qb.BalanceDelta, qb.BlockedDelta)
	}
	if len(r.NewOrders) != 1 || r.NewOrders[0].Status != repository.OrderStatusCancelled {
		t.Fatalf("NewOrders = %+v, want one cancelled", r.NewOrders)
	}
	if len(r.CancelledOrders) != 1 || r.CancelledOrders[0].OrderID != id {
		t.Fatalf("CancelledOrders = %+v, want one for %s", r.CancelledOrders, id)
	}
	assertConserved(t, r)

	if upd := findOrderUpdate(o.DrainStream(), id); upd == nil || upd.Status != statusExpired {
		t.Fatalf("stream status = %+v, want %q", upd, statusExpired)
	}
}

// Expired is the screen the matcher applies before matching; the boundary must agree with
// ExpireDue, which treats an expiry equal to now as already due.
func TestExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt *int64
		now       int64
		want      bool
	}{
		{"no TTL never expires", nil, math.MaxInt64, false},
		{"TTL in the future", unixPtr(1000), 999, false},
		{"TTL exactly now", unixPtr(1000), 1000, true},
		{"TTL in the past", unixPtr(1000), 1001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Expired(&oeq.OpenOrderEvent{ExpiresAt: tt.expiresAt}, tt.now); got != tt.want {
				t.Fatalf("Expired = %v, want %v", got, tt.want)
			}
		})
	}
}

// The sweep rides along with a live batch, so it must be bounded: after downtime a hydrated book can
// surface thousands of overdue orders, and retiring them all in one transaction would stall the real
// order behind them.
func TestExpireDue_RespectsTheLimit(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	for i := range 5 {
		restSellExpiring(o, seller, uint64(100+i), 10, unixPtr(1000))
	}

	if got := o.ExpireDue(2000, 2); len(got) != 2 {
		t.Fatalf("ExpireDue with limit 2 returned %d orders, want 2", len(got))
	}
	if got := o.ExpireDue(2000, maxExpirySweep); len(got) != 5 {
		t.Fatalf("ExpireDue uncapped returned %d orders, want all 5", len(got))
	}
}

// HasDue answers the sweep ticker's only question without allocating the id slice, so it must agree
// with ExpireDue on every boundary.
func TestHasDue(t *testing.T) {
	o := testBook()
	if o.HasDue(math.MaxInt64) {
		t.Fatal("an empty book has nothing due")
	}

	seller := uuid.New()
	restSell(o, seller, 100, 10) // no TTL
	if o.HasDue(math.MaxInt64) {
		t.Fatal("an order with no TTL is never due")
	}

	restSellExpiring(o, seller, 101, 10, unixPtr(1000))
	if o.HasDue(999) {
		t.Fatal("not due until its expiry")
	}
	if !o.HasDue(1000) {
		t.Fatal("due once its expiry is reached")
	}
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

	got := o.ExpireDue(1000, maxExpirySweep)
	if len(got) != 1 || got[0] != due {
		t.Fatalf("ExpireDue(1000) = %v, want just [%s]", got, due)
	}

	got = o.ExpireDue(2000, maxExpirySweep)
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

	if got := o.ExpireDue(math.MaxInt64, maxExpirySweep); len(got) != 0 {
		t.Fatalf("ExpireDue still reports a fully filled order: %v", got)
	}
}
