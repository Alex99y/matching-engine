package orderprocessors

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/core/internal/orderbook"
	"github.com/alex99y/matching-engine/core/pkg/deadletter"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
)

// This file is the consumer-goroutine boundary: decoding and validating a raw broker
// delivery into a queuedEvent for the matcher. It never touches the book, so there is no
// race with the matcher goroutine.

// handleDelivery runs on the consumer goroutine. It decodes and validates each delivery, parks the
// ones that can never be processed (see deadletter.go), and forwards the rest to the matcher.
func (o *OrderProcessor) handleDelivery(ctx context.Context, d *oeq.OrderDelivery) {
	qe, env := o.classify(d)
	if env != nil {
		o.deadLetter(ctx, d, env)
		if qe != nil && qe.open != nil {
			o.notifyDeadLettered(qe.open.UserID, qe.open.OrderID, orderbook.StatusDeadLettered)
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

// classify decodes a delivery into a processable event
func (o *OrderProcessor) classify(d *oeq.OrderDelivery) (*queuedEvent, *deadletter.Envelope) {
	if d.Event == nil {
		return nil, &deadletter.Envelope{
			Reason:  deadletter.ReasonMalformed,
			Error:   "envelope did not parse",
			Payload: d.Raw,
		}
	}

	switch d.Event.Type {
	case oeq.EventTypeOpenOrder:
		open, err := d.Event.DecodeOpenOrder()
		if err != nil {
			return nil, o.envelope(d, deadletter.ReasonMalformed, err.Error())
		}
		if err := oeq.ValidateOrderEvent(open, o.constraints); err != nil {
			env := o.envelope(d, deadletter.ReasonInvalid, err.Error())
			env.OrderID = open.OrderID.String()
			return &queuedEvent{delivery: d, open: open}, env
		}
		return &queuedEvent{delivery: d, open: open}, nil

	case oeq.EventTypeCancelOrder:
		cancel, err := d.Event.DecodeCancelOrder()
		if err != nil {
			return nil, o.envelope(d, deadletter.ReasonMalformed, err.Error())
		}
		return &queuedEvent{delivery: d, cancel: cancel}, nil

	default:
		return nil, o.envelope(d, deadletter.ReasonUnknownType,
			fmt.Sprintf("unknown event type %q", d.Event.Type))
	}
}

func (o *OrderProcessor) envelope(d *oeq.OrderDelivery, reason deadletter.Reason, cause string) *deadletter.Envelope {
	env := &deadletter.Envelope{Reason: reason, Error: cause, Payload: d.Raw}
	if d.Event != nil {
		env.EventType = string(d.Event.Type)
	}
	return env
}
