package orderprocessors

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/core/internal/orderbook"
	"github.com/alex99y/matching-engine/core/pkg/deadletter"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
)

var errNoDeadLetterPublisher = errors.New("no dead-letter publisher configured")

type deadLetterer interface {
	Publish(ctx context.Context, env *deadletter.Envelope) error
}

// deadLetter parks a command and acks its broker message, so it can never be redelivered.
func (o *OrderProcessor) deadLetter(ctx context.Context, d *oeq.OrderDelivery, env *deadletter.Envelope) {
	env.MarketRef = o.marketRef
	o.metrics.IncDeadLetter(string(env.Reason))

	if err := o.publishDeadLetter(ctx, env); err != nil {
		o.metrics.IncDLQPublishFailure()
		o.logger.Error(fmt.Sprintf(
			"order processor %s: DEAD-LETTER PUBLISH FAILED, dropping command reason=%s type=%q order=%q cause=%q payload=%s: %v",
			o.marketRef, env.Reason, env.EventType, env.OrderID, env.Error, env.Payload, err))
	}

	if d == nil {
		return
	}
	if err := d.Ack(); err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s: ack of dead-lettered message failed id=%s: %s",
			o.marketRef, d.ID(), err))
	}
}

func (o *OrderProcessor) publishDeadLetter(ctx context.Context, env *deadletter.Envelope) error {
	if o.dlq == nil {
		return errNoDeadLetterPublisher
	}
	return o.dlq.Publish(ctx, env)
}

// parkPoison handles an event that has failed to commit maxOrderFailures times.
// Runs on the matcher goroutine, from isolate.
func (o *OrderProcessor) parkPoison(ctx context.Context, qe *queuedEvent, key uuid.UUID, failures int, cause error) {
	env := &deadletter.Envelope{
		OrderID:  key.String(),
		Reason:   deadletter.ReasonPoison,
		Error:    cause.Error(),
		Failures: failures,
		Payload:  qe.delivery.Raw,
	}

	switch {
	case qe.cancel != nil:
		env.EventType = string(oeq.EventTypeCancelOrder)
	case qe.open != nil:
		env.EventType = string(oeq.EventTypeOpenOrder)
	}

	o.deadLetter(ctx, qe.delivery, env)

	switch {
	case qe.open != nil:
		o.notifyDeadLettered(qe.open.UserID, qe.open.OrderID, orderbook.StatusDeadLettered)
	case qe.cancel != nil:
		// The targeted order is still resting with its funds blocked, so the owner must be told the
		// cancel did not take. Its user id is not on the cancel event, so the book supplies it.
		if userID, ok := o.book.RestingOwner(qe.cancel.OrderID); ok {
			o.notifyDeadLettered(userID, qe.cancel.OrderID, orderbook.StatusCancelRejected)
		}
	}
}

// notifyDeadLettered emits a best-effort private stream event so the owner learns the order will
// never reach the book.
func (o *OrderProcessor) notifyDeadLettered(userID, orderID uuid.UUID, status string) {
	if userID == uuid.Nil || orderID == uuid.Nil {
		return
	}
	o.publishOrderUpdate(userID, orderID, status, 0, 0)
}
