// Package correlate matches order sends to a later condition observed on the private order
// stream, turning (send time, event time) pairs into latency samples.
package correlate

import (
	"sync"
	"time"

	"github.com/alex99y/matching-engine/loadtest/internal/stream"
	"github.com/google/uuid"
)

// Sample is one resolved (sent -> observed) latency measurement.
type Sample struct {
	OrderID uuid.UUID
	SentAt  time.Time
	Latency time.Duration
	Event   stream.OrderEvent
}

// DeadOrder is an order that reached a terminal status without ever satisfying the tracker's
// condition — e.g. a "rejected" event fed to a tracker watching for a fill. It is reported
// separately from Timeouts() because it resolved definitively; it will never produce a sample no
// matter how long the tracker waits, unlike a genuinely slow/lost order.
type DeadOrder struct {
	OrderID uuid.UUID
	SentAt  time.Time
	Status  string
}

type pendingOrder struct {
	sentAt time.Time
	done   chan struct{} // closed once this order resolves, for Await
}

// terminalStatuses are statuses after which the private stream will never emit another event for
// that order. partially_filled is deliberately excluded: a resting partial fill can still receive
// more fills or a later cancel.
var terminalStatuses = map[string]bool{
	stream.StatusFilled:    true,
	stream.StatusCancelled: true,
	stream.StatusRejected:  true,
}

// Tracker watches a stream of OrderEvents for a caller-supplied condition (Match), keyed by
// order id. Every tracked order resolves into exactly one of three buckets: Samples (the
// condition was met), Dead (a terminal event arrived that will never meet it), or Timeouts (still
// pending when Sweep was called past its deadline) — so no sent order is ever silently unaccounted
// for in the final report.
type Tracker struct {
	match func(stream.OrderEvent) bool

	mu       sync.Mutex
	pending  map[uuid.UUID]pendingOrder
	samples  []Sample
	dead     []DeadOrder
	timeouts []DeadOrder
}

func NewTracker(match func(stream.OrderEvent) bool) *Tracker {
	return &Tracker{
		match:   match,
		pending: make(map[uuid.UUID]pendingOrder),
	}
}

// Track begins watching orderID, correlated against sentAt (the timestamp meaningful for this
// test — e.g. the original order's send time, or a later cancel's send time).
func (t *Tracker) Track(orderID uuid.UUID, sentAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[orderID] = pendingOrder{sentAt: sentAt}
}

// Feed processes one observed event. It is a no-op for orders this tracker isn't watching (either
// never tracked, or already resolved).
func (t *Tracker) Feed(event stream.OrderEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, ok := t.pending[event.OrderID]
	if !ok {
		return
	}

	if t.match(event) {
		delete(t.pending, event.OrderID)
		t.samples = append(t.samples, Sample{
			OrderID: event.OrderID,
			SentAt:  p.sentAt,
			Latency: event.ReceivedAt.Sub(p.sentAt),
			Event:   event,
		})
		return
	}

	if terminalStatuses[event.Status] {
		delete(t.pending, event.OrderID)
		t.dead = append(t.dead, DeadOrder{OrderID: event.OrderID, SentAt: p.sentAt, Status: event.Status})
	}
}

// Sweep moves every order still pending with sentAt before cutoff into Timeouts. Call this
// periodically (or once at the end of a run) so orders that never got a matching event — e.g. a
// stream reconnect swallowed it — are reported instead of leaking in `pending` forever.
func (t *Tracker) Sweep(cutoff time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, p := range t.pending {
		if p.sentAt.Before(cutoff) {
			delete(t.pending, id)
			t.timeouts = append(t.timeouts, DeadOrder{OrderID: id, SentAt: p.sentAt})
		}
	}
}

func (t *Tracker) Samples() []Sample {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Sample(nil), t.samples...)
}

func (t *Tracker) Dead() []DeadOrder {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]DeadOrder(nil), t.dead...)
}

func (t *Tracker) Timeouts() []DeadOrder {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]DeadOrder(nil), t.timeouts...)
}

func (t *Tracker) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}
