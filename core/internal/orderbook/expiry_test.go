package orderbook

import (
	"math"
	"testing"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

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
