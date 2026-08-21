package orderprocessors

import (
	"fmt"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
)

// This file is the consumer-goroutine boundary: decoding and validating a raw broker
// delivery into a queuedEvent for the matcher. It never touches the book, so there is no
// race with the matcher goroutine.

// handleDelivery runs on the consumer goroutine. It decodes and validates each
// delivery, dropping (acking) malformed or invalid ones, and forwards the rest to the
// matcher.
func (o *OrderProcessor) handleDelivery(d *oeq.OrderDelivery) {
	qe, ok := o.classify(d)
	if !ok {
		// Malformed/invalid/unknown — drop it. It has no DB effect, so acking now
		// (independently of any batch) is safe and avoids an infinite requeue.
		if err := d.Ack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: ack of dropped message failed: %s", err))
		}
		return
	}
	if o.stopMatcher.Load() {
		if err := d.Nack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: nack while stopping failed: %s", err))
		}
		return
	}
	if qe.open != nil {
		o.metrics.IncReceived()
	}
	o.ordersChannel <- qe
}

func (o *OrderProcessor) classify(d *oeq.OrderDelivery) (*queuedEvent, bool) {
	switch d.Event.Type {
	case oeq.EventTypeOpenOrder:
		open, err := d.Event.DecodeOpenOrder()
		if err != nil {
			o.logger.Warn(fmt.Sprintf("order processor: malformed open_order payload: %v", err))
			return nil, false
		}
		if err := oeq.ValidateOrderEvent(open, o.constraints); err != nil {
			o.logger.Warn(fmt.Sprintf("order processor: invalid order from publisher: %s", err))
			return nil, false
		}
		return &queuedEvent{delivery: d, open: open}, true

	case oeq.EventTypeCancelOrder:
		cancel, err := d.Event.DecodeCancelOrder()
		if err != nil {
			o.logger.Warn(fmt.Sprintf("order processor: malformed cancel_order payload: %v", err))
			return nil, false
		}
		return &queuedEvent{delivery: d, cancel: cancel}, true

	default:
		o.logger.Warn(fmt.Sprintf("order processor: unknown event type %q — dropping", d.Event.Type))
		return nil, false
	}
}
