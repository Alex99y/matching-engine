package order_events_queue

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
)

type OrdersEventsQueue struct {
	marketRef string
	queue     *rabbitmq.Queue
	logger    *logger.Logger
}

func (o *OrdersEventsQueue) EmitNewOrderToME(ctx context.Context, order *OrderEvent) error {
	raw, err := order.ToBytes()
	if err != nil {
		return fmt.Errorf("emit order: %w", err)
	}
	if err := o.queue.Publish(ctx, raw, true); err != nil {
		return fmt.Errorf("emit order: publish: %w", err)
	}
	return nil
}

// OrderDelivery couples a parsed order event with its broker acknowledgement
// controls. Under ack-after-commit the matcher acknowledges a message only once the
// batch containing it is durably committed, so ack/nack are deferred to the matcher
// rather than performed here by the consumer.
type OrderDelivery struct {
	Event *OrderEvent
	id    string
	ack   func() error
	nack  func() error
}

func (d *OrderDelivery) ID() string  { return d.id }
func (d *OrderDelivery) Ack() error  { return d.ack() }
func (d *OrderDelivery) Nack() error { return d.nack() }

// NewOrderDelivery builds a delivery from an already-parsed event and its ack/nack
// controls. The consumer constructs deliveries directly; this is the exported path
// used by callers (and tests) that supply their own acknowledgement hooks.
func NewOrderDelivery(event *OrderEvent, id string, ack, nack func() error) *OrderDelivery {
	return &OrderDelivery{Event: event, id: id, ack: ack, nack: nack}
}

// OrderDeliveryHandler receives each successfully parsed delivery. It must not block
// for long: it hands the delivery off to the matcher and returns. Ownership of the
// eventual ack/nack passes to whoever holds the OrderDelivery.
type OrderDeliveryHandler func(*OrderDelivery)

// WatchForOrderEvents consumes the market's command queue. It parses each envelope,
// dead-letters malformed ones, and forwards the rest to the handler without
// acknowledging — the matcher acks/nacks after the batch commits.
func (o *OrdersEventsQueue) WatchForOrderEvents(ctx context.Context, handler OrderDeliveryHandler) error {
	return o.queue.Consume(ctx, func(args *rabbitmq.ConsumeArgs) {
		event, err := ParseOrderEvent(args.RawMessage())
		if err != nil {
			// API sent a malformed message — reject without requeue (dead-letter it, do not retry).
			o.logger.Error(fmt.Sprintf("order_events_queue: malformed message id=%s: %v", args.Id(), err))
			if rejectErr := args.Reject(); rejectErr != nil {
				o.logger.Error(fmt.Sprintf("order_events_queue: reject failed id=%s: %v", args.Id(), rejectErr))
			}
			return
		}
		handler(&OrderDelivery{
			Event: event,
			id:    args.Id(),
			ack:   args.Ack,
			nack:  args.Nack,
		})
	})
}

func NewOrdersQueue(
	logger *logger.Logger,
	marketRef string,
	rabbitMqClient *rabbitmq.RabbitMQClient,
) *OrdersEventsQueue {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if rabbitMqClient == nil {
		panic("rabbitMqClient cannot be nil")
	}

	queue, err := rabbitmq.NewQueue(
		rabbitMqClient,
		rabbitmq.ChannelArgs{
			PrefetchCount: 16,
			PrefetchSize:  0,
		},
		rabbitmq.QueueArgs{
			Name:       marketRef,
			Durable:    true,
			AutoDelete: false,
			Exclusive:  false,
			NoWait:     false,
		},
		logger,
	)
	if err != nil {
		panic(fmt.Sprintf("order_events_queue: couldn't create queue for market %s: %v", marketRef, err))
	}

	return &OrdersEventsQueue{
		marketRef: marketRef,
		logger:    logger,
		queue:     queue,
	}
}
