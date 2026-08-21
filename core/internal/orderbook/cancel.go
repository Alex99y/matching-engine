package orderbook

import (
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file covers a resting order leaving the book other than by a fill: a user cancel
// (CancelOrder) or a TTL reap (ExpireOrder — see expiry.go for the due-order index). Both
// share removeResting (detach from the book) and closeResting (release funds, record the
// terminal status) and differ only in why the order left.

// CancelOrder removes a resting order, releases its remaining reservation and records
// the cancellation. A miss is a normal logical race (the order may have filled, never
// existed, or already been cancelled) and is an idempotent no-op.
func (o *OrderBook) CancelOrder(event *oeq.CancelOrderEvent, result *repository.BatchResult) {
	stored, ok := o.removeResting(event.OrderID)
	if !ok {
		// @TODO(P-events): emit cancel-reject event once Queue 2 exists.
		return
	}
	o.closeResting(stored, result, "")
}

// ExpireOrder has the same book/fund/persistence effects as CancelOrder — only the live
// stream's reported reason differs (statusExpired), so a subscriber can tell a TTL reap
// apart from a user cancel. A miss (order filled/cancelled between ExpireDue collecting its
// id and this call) is an idempotent no-op, same as CancelOrder.
func (o *OrderBook) ExpireOrder(orderID uuid.UUID, result *repository.BatchResult) {
	stored, ok := o.removeResting(orderID)
	if !ok {
		return
	}
	o.closeResting(stored, result, statusExpired)
}

// removeResting detaches a resting order from its price level, the order index, and the
// expiry index (if it carries a TTL). ok is false if the order is no longer resting.
func (o *OrderBook) removeResting(orderID uuid.UUID) (*Order, bool) {
	loc, ok := o.index[orderID]
	if !ok {
		return nil, false
	}

	stored, ok := loc.el.Value.(*Order)
	if !ok {
		o.logger.Error("orderbook: corrupt list element in removeResting")
		return nil, false
	}

	loc.level.Orders.Remove(loc.el)
	loc.level.TotalQty -= stored.Remaining
	o.markLevel(loc.side, loc.level.Price)
	delete(o.index, orderID)
	if loc.level.Orders.Len() == 0 {
		o.sideTree(loc.side).Delete(loc.level)
	}
	o.unindexExpiry(stored)

	return stored, true
}

// closeResting releases a resting order's unused reservation and records its terminal
// status — shared by CancelOrder and ExpireOrder. streamReason overrides the live-stream
// status when non-empty (see statusExpired); the persisted DB status and the reported
// filled/remaining amounts are unaffected either way.
func (o *OrderBook) closeResting(stored *Order, result *repository.BatchResult, streamReason string) {
	have, want := restingRemaining(stored, o.market.BaseScale)

	if stored.OpenOrder.Side == oeq.BuyOrder {
		result.AddBalanceDelta(stored.OpenOrder.UserID, o.quoteInstr(), int64(have), -int64(have))
	} else {
		result.AddBalanceDelta(stored.OpenOrder.UserID, o.baseInstr(), int64(have), -int64(have))
	}

	result.ClosedOpenOrders = append(result.ClosedOpenOrders, stored.OpenOrder.OrderID)
	result.CancelledOrders = append(result.CancelledOrders, repository.InsertCancelledOrderParams{
		OrderID:             stored.OpenOrder.OrderID,
		RemainingHaveAmount: have,
		RemainingWantAmount: want,
	})

	// A partially filled order keeps the "partially_filled" terminal status; one that
	// never traded becomes "cancelled". (Original qty is lost after hydration, so a
	// rebuilt order always reports "cancelled" — see Hydrate.)
	status := repository.OrderStatusCancelled
	if stored.Remaining < stored.OpenOrder.Quantity {
		status = repository.OrderStatusPartiallyFilled
	}
	result.StatusUpdates = append(result.StatusUpdates, repository.OrderStatusUpdate{
		OrderID: stored.OpenOrder.OrderID,
		Status:  status,
	})

	streamStatus := status
	if streamReason != "" {
		streamStatus = streamReason
	}
	o.recordOrderUpdate(stored.OpenOrder.UserID, stored.OpenOrder.OrderID, streamStatus,
		stored.OpenOrder.Quantity-stored.Remaining, stored.Remaining)
}
