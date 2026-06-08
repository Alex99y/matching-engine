package orderqueue

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/core/pkg/order_events_queue"
)

var ErrMarketQueueNotFound = errors.New("no queue registered for market")

type OrderCommandPublisher struct {
	queues map[string]*order_events_queue.OrdersEventsQueue
	logger *logger.Logger
}

func (p *OrderCommandPublisher) Publish(ctx context.Context, marketRef string, event *order_events_queue.OrderEvent) error {
	q, ok := p.queues[marketRef]
	if !ok {
		return fmt.Errorf("%w: %q", ErrMarketQueueNotFound, marketRef)
	}
	if err := q.EmitNewOrderToME(ctx, event); err != nil {
		return fmt.Errorf("publish to market %q: %w", marketRef, err)
	}
	return nil
}

// NewOrderCommandPublisher creates one RabbitMQ queue per market ref at startup.
// Panics if any queue cannot be declared — this is a fatal misconfiguration.
func NewOrderCommandPublisher(
	log *logger.Logger,
	rabbitMQClient *rabbitmq.RabbitMQClient,
	marketRefs []string,
) *OrderCommandPublisher {
	if log == nil {
		panic("logger cannot be nil")
	}
	if rabbitMQClient == nil {
		panic("rabbitMQClient cannot be nil")
	}
	queues := make(map[string]*order_events_queue.OrdersEventsQueue, len(marketRefs))
	for _, ref := range marketRefs {
		queues[ref] = order_events_queue.NewOrdersQueue(log, ref, rabbitMQClient)
	}
	return &OrderCommandPublisher{
		queues: queues,
		logger: log,
	}
}
