package orderprocessors

import (
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/google/uuid"
)

// This file holds the event-log stream side of the processor (docs/event-log.md): turning a
// committed batch's accumulated events — plus periodic snapshots and heartbeats — into sequenced
// envelopes on the me.events exchange. Every method here runs on the matcher goroutine and is
// best-effort: it never blocks matching and drops on back-pressure, leaving the API to re-sync from
// the next snapshot via the sequence gap.

// publishStream drains the events the just-committed batch accumulated in the book and publishes
// them. Only book deltas advance seq (the API's cache sync keys off it); trades, order updates,
// snapshots and heartbeats are stamped with the current seq for reference. Order is book → trades →
// private orders, so the book's sequence advances before anything references it.
func (o *OrderProcessor) publishStream() {
	if o.publisher == nil {
		return
	}
	s := o.book.DrainStream()
	now := time.Now().UnixMilli()

	for _, delta := range s.Book {
		o.seq++
		o.publish(marketdata.EventBook, o.seq, marketdata.PublicKey(o.marketRef, marketdata.EventBook), now, delta)
	}
	for _, trade := range s.Trades {
		o.publish(marketdata.EventTrade, o.seq, marketdata.PublicKey(o.marketRef, marketdata.EventTrade), now, trade)
	}
	for _, ou := range s.Orders {
		o.publish(marketdata.EventOrder, o.seq, marketdata.PrivateKey(ou.UserID.String(), marketdata.EventOrder), now, ou.Update)
	}
}

// emitSnapshot broadcasts the full L2 book at the current sequence point so a fresh or recovering
// API can bootstrap its cache (docs/event-log.md §4). It does not advance seq — it is a marker at
// the current seq, not a delta. Reads the book on the matcher goroutine (the owner), so no lock.
func (o *OrderProcessor) emitSnapshot() {
	if o.publisher == nil {
		return
	}
	bids, asks := o.book.SnapshotLevels()
	snap := marketdata.Snapshot{
		Epoch:  o.epoch,
		Seq:    o.seq,
		Market: o.marketRef,
		Bids:   bids,
		Asks:   asks,
	}
	o.publish(marketdata.EventSnapshot, o.seq, marketdata.PublicKey(o.marketRef, marketdata.EventSnapshot), time.Now().UnixMilli(), snap)
}

// emitHeartbeat announces liveness and the current seq so an idle consumer can detect a gap without
// waiting for the next delta. It does not advance seq.
func (o *OrderProcessor) emitHeartbeat() {
	if o.publisher == nil {
		return
	}
	o.publish(marketdata.EventHeartbeat, o.seq, marketdata.PublicKey(o.marketRef, marketdata.EventHeartbeat), time.Now().UnixMilli(), marketdata.Heartbeat{})
}

// publish serializes one envelope and hands it to the async publisher. A serialization failure is
// logged and dropped (it must never wedge the matcher); a full publisher buffer drops silently and
// the consumer re-syncs from the next snapshot.
func (o *OrderProcessor) publish(t marketdata.EventType, seq uint64, routingKey string, tsMillis int64, payload any) {
	env, err := marketdata.NewEnvelope(o.epoch, seq, t, o.marketRef, tsMillis, payload)
	if err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s: build %s envelope: %s", o.marketRef, t, err))
		return
	}
	body, err := env.ToBytes()
	if err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s: serialize %s envelope: %s", o.marketRef, t, err))
		return
	}
	o.publisher.Enqueue(routingKey, uuid.NewString(), body)
}
