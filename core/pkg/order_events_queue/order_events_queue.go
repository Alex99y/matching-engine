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

func (o *OrdersEventsQueue) WatchForOrderEvents(ctx context.Context, callback OrderEventConsumeCallback) error {
	return o.queue.Consume(ctx, func(args *rabbitmq.ConsumeArgs) {
		order, err := ParseOrderEvent(args.RawMessage())
		if err != nil {
			// API sent a malformed message — reject without requeue (dead-letter it, do not retry).
			o.logger.Error(fmt.Sprintf("order_events_queue: malformed message id=%s: %v", args.Id(), err))
			if rejectErr := args.Reject(); rejectErr != nil {
				o.logger.Error(fmt.Sprintf("order_events_queue: reject failed id=%s: %v", args.Id(), rejectErr))
			}
			return
		}
		if err := callback(order); err != nil {
			// Transient processing failure — nack and requeue for retry.
			o.logger.Error(fmt.Sprintf("order_events_queue: processing failed id=%s: %v", args.Id(), err))
			if nackErr := args.Nack(); nackErr != nil {
				o.logger.Error(fmt.Sprintf("order_events_queue: nack failed id=%s: %v", args.Id(), nackErr))
			}
			return
		}
		if ackErr := args.Ack(); ackErr != nil {
			o.logger.Error(fmt.Sprintf("order_events_queue: ack failed id=%s: %v", args.Id(), ackErr))
		}
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
