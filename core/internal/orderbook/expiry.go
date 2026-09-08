package orderbook

import (
	"bytes"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
)

// This file holds the TTL index: which resting orders carry an ExpiresAt, and finding the
// ones that are due. Removing a due order is ExpireOrder, in cancel.go.

// expiryEntry indexes one resting order's TTL for ExpireDue. orderID breaks ties between
// orders that share the same expiry second, giving the btree a total order.
type expiryEntry struct {
	expiresAt int64
	orderID   uuid.UUID
}

func expiryLess(a, b *expiryEntry) bool {
	if a.expiresAt != b.expiresAt {
		return a.expiresAt < b.expiresAt
	}
	return bytes.Compare(a.orderID[:], b.orderID[:]) < 0
}

// Expired reports whether an order's TTL has elapsed as of now (unix seconds).
func Expired(event *oeq.OpenOrderEvent, now int64) bool {
	return event.ExpiresAt != nil && *event.ExpiresAt <= now
}

// ExpireDue returns the ids of at most limit resting orders whose TTL has elapsed as of now (unix seconds).
func (o *OrderBook) ExpireDue(now int64, limit int) []uuid.UUID {
	var due []uuid.UUID
	o.expiries.Ascend(func(e *expiryEntry) bool {
		if e.expiresAt > now || len(due) == limit {
			return false
		}
		due = append(due, e.orderID)
		return true
	})
	return due
}

// HasDue reports whether any resting order is due
func (o *OrderBook) HasDue(now int64) bool {
	due := false
	o.expiries.Ascend(func(e *expiryEntry) bool {
		due = e.expiresAt <= now
		return false
	})
	return due
}

func (o *OrderBook) indexExpiry(order *Order) {
	if order.OpenOrder.ExpiresAt == nil {
		return
	}
	o.expiries.ReplaceOrInsert(&expiryEntry{expiresAt: *order.OpenOrder.ExpiresAt, orderID: order.OpenOrder.OrderID})
}

func (o *OrderBook) unindexExpiry(order *Order) {
	if order.OpenOrder.ExpiresAt == nil {
		return
	}
	o.expiries.Delete(&expiryEntry{expiresAt: *order.OpenOrder.ExpiresAt, orderID: order.OpenOrder.OrderID})
}
