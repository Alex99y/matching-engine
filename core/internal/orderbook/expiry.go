package orderbook

import (
	"bytes"

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

// ExpireDue returns the ids of every resting order whose TTL has elapsed as of now (unix
// seconds). It is a pure read (no mutation, no I/O): the index is sorted by expiry time, so
// this walks only the *due* prefix and stops at the first order that isn't — O(k log n) for
// k due orders, not O(book size). The caller removes each returned id via ExpireOrder inside
// the batch/transaction that persists the change.
func (o *OrderBook) ExpireDue(now int64) []uuid.UUID {
	var due []uuid.UUID
	o.expiries.Ascend(func(e *expiryEntry) bool {
		if e.expiresAt > now {
			return false
		}
		due = append(due, e.orderID)
		return true
	})
	return due
}

// indexExpiry adds a just-rested order to the expiry index, if it carries a TTL.
func (o *OrderBook) indexExpiry(order *Order) {
	if order.OpenOrder.ExpiresAt == nil {
		return
	}
	o.expiries.ReplaceOrInsert(&expiryEntry{expiresAt: *order.OpenOrder.ExpiresAt, orderID: order.OpenOrder.OrderID})
}

// unindexExpiry removes an order leaving the book from the expiry index, if it carries a TTL.
func (o *OrderBook) unindexExpiry(order *Order) {
	if order.OpenOrder.ExpiresAt == nil {
		return
	}
	o.expiries.Delete(&expiryEntry{expiresAt: *order.OpenOrder.ExpiresAt, orderID: order.OpenOrder.OrderID})
}
