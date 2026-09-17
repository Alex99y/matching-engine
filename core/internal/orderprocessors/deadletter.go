package orderprocessors

import (
	"fmt"

	"github.com/alex99y/matching-engine/core/internal/orderbook"
	"github.com/alex99y/matching-engine/core/pkg/deadletter"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
)

// poisonRecorder hands the consumer of this market's dead-letter queue the one fact it cannot
// recompute from the payload: why a well-formed order failed to commit. See deadletter.PoisonErrors.
type poisonRecorder interface {
	Record(messageID, eventType, errText string)
}

// deadLetter rejects a command without requeue. The command queue's dead-letter exchange moves it
// to the market's parking queue in the same broker operation, so it is never redelivered here and
// never lost; the deadletter.Consumer records it from there. A failed reject leaves the message
// unacked, and the broker redelivers it — the outcome is the same, later.
func (o *OrderProcessor) deadLetter(d *oeq.OrderDelivery, reason deadletter.Reason) {
	o.metrics.IncDeadLetter(string(reason))
	if err := d.Reject(); err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s: reject of dead-lettered message failed id=%s: %s",
			o.marketRef, d.ID(), err))
	}
}

// parkPoison handles an event that has failed to commit maxOrderFailures times.
// Runs on the matcher goroutine, from isolate.
func (o *OrderProcessor) parkPoison(qe *queuedEvent, cause error) {
	var eventType oeq.EventType
	switch {
	case qe.cancel != nil:
		eventType = oeq.EventTypeCancelOrder
	case qe.open != nil:
		eventType = oeq.EventTypeOpenOrder
	}
	// Recorded before the reject so the consumer can never see the message first.
	o.poison.Record(qe.delivery.ID(), string(eventType), cause.Error())
	o.deadLetter(qe.delivery, deadletter.ReasonPoison)

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
