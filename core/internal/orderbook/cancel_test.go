package orderbook

import (
	"math"
	"testing"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

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

// Cancelling one of two orders sharing a price must leave the level in the tree carrying the
// survivor's quantity — removeResting only deletes a level once it is empty.
func TestCancelOrder_LeavesSharedLevelIntact(t *testing.T) {
	o := testBook()
	restSell(o, uuid.New(), 100, 4)
	victim := restSell(o, uuid.New(), 100, 6)

	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: victim}, repository.NewBatchResult())

	_, asks := o.SnapshotLevels()
	if len(asks) != 1 || asks[0].Price != 100 || asks[0].Quantity != 4 {
		t.Fatalf("asks = %+v, want the 100 level surviving with qty 4", asks)
	}
	if s := o.Stats(); s.AskOrders != 1 {
		t.Fatalf("askOrders = %d, want 1", s.AskOrders)
	}
}

// A cancel for an order that is not resting (already filled, already cancelled, or never
// existed) is a normal logical race and must be an idempotent no-op, same as ExpireOrder's miss.
func TestCancelOrder_UnknownOrderIsNoop(t *testing.T) {
	o := testBook()
	r := repository.NewBatchResult()
	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: uuid.New()}, r)

	if len(r.ClosedOpenOrders) != 0 || len(r.StatusUpdates) != 0 || len(r.BalanceDeltas()) != 0 {
		t.Fatalf("CancelOrder on an unknown id mutated the result: %+v", r)
	}
}
