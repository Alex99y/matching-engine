package orderbook

import (
	"bytes"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file is the bracket-exit index: the exit an entry with a take-profit / stop-loss arms
// when it completes, parked outside the book until the last trade price reaches a trigger.
// Arming happens at the entry's terminal points (match.go, cancel.go); the matcher drives firing
// through TriggeredExits / FireExit, and a user cancel reaches a parked exit via CancelOrder.

// triggerEntry indexes one trigger price of one pending exit. orderID breaks ties between exits
// sharing a price, giving the btree a total order.
type triggerEntry struct {
	price   uint64
	orderID uuid.UUID
}

// fireAbove is ascending and fireBelow descending, so in both trees the minimum is the first
// trigger a moving price reaches and "anything due?" is a single peek.
func triggerAboveLess(a, b *triggerEntry) bool {
	if a.price != b.price {
		return a.price < b.price
	}
	return bytes.Compare(a.orderID[:], b.orderID[:]) < 0
}

func triggerBelowLess(a, b *triggerEntry) bool {
	if a.price != b.price {
		return a.price > b.price
	}
	return bytes.Compare(a.orderID[:], b.orderID[:]) < 0
}

// pendingExit is a parked exit and its index entries: above fires once lastPrice >= its price,
// below once lastPrice <= its price. An exit with both triggers has both; whichever fires first
// removes the other.
type pendingExit struct {
	event *oeq.OpenOrderEvent
	above *triggerEntry
	below *triggerEntry
}

// exitTriggers splits an exit's take-profit / stop-loss into the direction each fires in. A sell
// exit (closing a bought position) takes profit on the way up and stops on the way down; a buy
// exit is the mirror image.
func exitTriggers(exit *oeq.OpenOrderEvent) (above, below *uint64) {
	if exit.Side == oeq.SellOrder {
		return exit.TakeProfitPrice, exit.StopLossPrice
	}
	return exit.StopLossPrice, exit.TakeProfitPrice
}

func (o *OrderBook) SetLastPrice(price uint64) {
	o.lastPrice, o.hasLast = price, true
}

// TriggeredExits returns the ids of at most limit pending exits whose trigger the last trade
// price has reached. Nothing is due until the market has traded at least once.
func (o *OrderBook) TriggeredExits(limit int) []uuid.UUID {
	if !o.hasLast {
		return nil
	}
	var due []uuid.UUID
	o.fireAbove.Ascend(func(e *triggerEntry) bool {
		if e.price > o.lastPrice || len(due) == limit {
			return false
		}
		due = append(due, e.orderID)
		return true
	})
	o.fireBelow.Ascend(func(e *triggerEntry) bool {
		if e.price < o.lastPrice || len(due) == limit {
			return false
		}
		due = append(due, e.orderID)
		return true
	})
	return due
}

func (o *OrderBook) HasTriggered() bool {
	if !o.hasLast {
		return false
	}
	if e, ok := o.fireAbove.Min(); ok && e.price <= o.lastPrice {
		return true
	}
	if e, ok := o.fireBelow.Min(); ok && e.price >= o.lastPrice {
		return true
	}
	return false
}

// FireExit sends a pending exit into the book. Its funds were blocked fill by fill as the entry
// traded, so it matches like any funded taker; only its orders row is updated rather than
// inserted. A miss is an idempotent no-op, same as CancelOrder.
func (o *OrderBook) FireExit(orderID uuid.UUID, result *repository.BatchResult) {
	p, ok := o.removePending(orderID)
	if !ok {
		return
	}
	taker := newOrder(p.event, o.market.BaseScale)
	taker.persisted = true
	o.matchTaker(taker, result)
}

// HydratePending rebuilds the trigger index from persisted pending exits. Their funds are
// already blocked, so nothing but the index changes.
func (o *OrderBook) HydratePending(rows []repository.PendingOrderHydration) {
	for _, r := range rows {
		parent := r.ParentOrderID
		exit := &oeq.OpenOrderEvent{
			OrderID:         r.OrderID,
			MarketID:        o.market.ID,
			UserID:          r.UserID,
			Side:            oeq.OrderSide(r.Side),
			Type:            oeq.MarketOrder,
			TimeInForce:     oeq.ImmediateOrCancel,
			TakeProfitPrice: r.TakeProfitPrice,
			StopLossPrice:   r.StopLossPrice,
			ParentOrderID:   &parent,
		}
		setExitAmount(exit, r.Amount)
		o.indexPending(exit)
	}
}

// armExit parks the exit of an entry that just reached a terminal state. Nothing to park for an
// order without triggers or one that never traded. The exit is a market IOC for exactly what the
// entry received — funds that are already blocked (see credit) — persisted as pending.
func (o *OrderBook) armExit(entry *Order, result *repository.BatchResult) {
	if !entry.hasExit() || entry.received == 0 {
		return
	}
	exit := o.exitEvent(entry)
	insert := DeriveInsertParams(exit, o.market)
	insert.Status = repository.OrderStatusPending
	result.NewOrders = append(result.NewOrders, insert)
	o.indexPending(exit)

	// A quote-denominated buy exit has no base remainder to report, same as emitTakerOutcome.
	o.recordOrderUpdate(exit.UserID, exit.OrderID, repository.OrderStatusPending, 0, exit.Quantity)
}

func (o *OrderBook) exitEvent(entry *Order) *oeq.OpenOrderEvent {
	parent := entry.OpenOrder.OrderID
	exit := &oeq.OpenOrderEvent{
		OrderID:         oeq.ExitOrderID(parent),
		MarketID:        entry.OpenOrder.MarketID,
		UserID:          entry.OpenOrder.UserID,
		Side:            oppositeSide(entry.OpenOrder.Side),
		Type:            oeq.MarketOrder,
		TimeInForce:     oeq.ImmediateOrCancel,
		TakeProfitPrice: entry.OpenOrder.TakeProfitPrice,
		StopLossPrice:   entry.OpenOrder.StopLossPrice,
		ParentOrderID:   &parent,
	}
	setExitAmount(exit, entry.received)
	return exit
}

// setExitAmount denominates the exit the way a market order of its side is: a sell offers base,
// a buy spends a quote budget.
func setExitAmount(exit *oeq.OpenOrderEvent, amount uint64) {
	if exit.Side == oeq.BuyOrder {
		exit.QuoteQty = &amount
		return
	}
	exit.Quantity = amount
}

func (o *OrderBook) indexPending(exit *oeq.OpenOrderEvent) {
	p := &pendingExit{event: exit}
	above, below := exitTriggers(exit)
	if above != nil {
		p.above = &triggerEntry{price: *above, orderID: exit.OrderID}
		o.fireAbove.ReplaceOrInsert(p.above)
	}
	if below != nil {
		p.below = &triggerEntry{price: *below, orderID: exit.OrderID}
		o.fireBelow.ReplaceOrInsert(p.below)
	}
	o.pending[exit.OrderID] = p
}

func (o *OrderBook) removePending(orderID uuid.UUID) (*pendingExit, bool) {
	p, ok := o.pending[orderID]
	if !ok {
		return nil, false
	}
	if p.above != nil {
		o.fireAbove.Delete(p.above)
	}
	if p.below != nil {
		o.fireBelow.Delete(p.below)
	}
	delete(o.pending, orderID)
	return p, true
}

// closePending is the cancel path of a parked exit: its whole blocked amount goes back and it
// is recorded cancelled. It never traded, so there is no partial fill to preserve.
func (o *OrderBook) closePending(p *pendingExit, result *repository.BatchResult) {
	exit := newOrder(p.event, o.market.BaseScale)
	releaseBlocked(result, exit.OpenOrder.UserID, o.haveInstr(exit.OpenOrder.Side), exit.reserve)

	result.StatusUpdates = append(result.StatusUpdates, repository.OrderStatusUpdate{
		OrderID: exit.OpenOrder.OrderID,
		Status:  repository.OrderStatusCancelled,
	})
	have, want := canceledRemaining(exit, o.market.BaseScale)
	result.CancelledOrders = append(result.CancelledOrders, repository.InsertCancelledOrderParams{
		OrderID:             exit.OpenOrder.OrderID,
		RemainingHaveAmount: have,
		RemainingWantAmount: want,
	})
	o.recordOrderUpdate(exit.OpenOrder.UserID, exit.OpenOrder.OrderID, repository.OrderStatusCancelled, 0, exit.Remaining)
}
