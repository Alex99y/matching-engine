package deadletter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

var ErrConsumerClosed = errors.New("dead letter consumer closed")

// persistBackoff paces retries when the database refuses a dead letter, so a sick database cannot
// spin the consumer through the same delivery.
const persistBackoff = time.Second

type deadLetterRepository interface {
	InsertDeadLetter(ctx context.Context, p repository.InsertDeadLetterParams) error
}

type persistMetrics interface {
	IncDLQPersistFailure()
}

// Consumer drains one market's parking queue into the dead_letters table. It runs in the same
// process as the matcher that rejects the messages, which is what lets it pick up poison error
// texts from PoisonErrors.
type Consumer struct {
	logger      *logger.Logger
	queue       *rabbitmq.Queue
	marketRef   string
	constraints oeq.MarketConstraints
	poison      *PoisonErrors
	repo        deadLetterRepository
	metrics     persistMetrics
}

// NewConsumer declares the exchange, the market's parking queue and its binding. metrics may be nil.
func NewConsumer(
	log *logger.Logger,
	client *rabbitmq.RabbitMQClient,
	marketRef string,
	constraints oeq.MarketConstraints,
	poison *PoisonErrors,
	repo deadLetterRepository,
	metrics persistMetrics,
) (*Consumer, error) {
	if log == nil {
		panic("logger cannot be nil")
	}
	if client == nil {
		panic("rabbitMqClient cannot be nil")
	}
	if poison == nil {
		panic("poison errors cannot be nil")
	}
	if repo == nil {
		panic("dead letter repository cannot be nil")
	}
	if err := DeclareTopology(client, marketRef); err != nil {
		return nil, fmt.Errorf("dead letter consumer %q: %w", marketRef, err)
	}
	queue, err := rabbitmq.NewQueue(
		client,
		rabbitmq.ChannelArgs{PrefetchCount: 1},
		rabbitmq.QueueArgs{
			Name:     QueueName(marketRef),
			Durable:  true,
			Bindings: []rabbitmq.Binding{{Exchange: ExchangeName, RoutingKey: marketRef}},
		},
		log,
	)
	if err != nil {
		return nil, fmt.Errorf("dead letter consumer %q: %w", marketRef, err)
	}
	return &Consumer{
		logger:      log,
		queue:       queue,
		marketRef:   marketRef,
		constraints: constraints,
		poison:      poison,
		repo:        repo,
		metrics:     metrics,
	}, nil
}

// Run blocks until ctx is cancelled. Call it in its own goroutine from main.
func (c *Consumer) Run(ctx context.Context) {
	err := c.queue.Consume(ctx, func(args *rabbitmq.ConsumeArgs) { c.handle(ctx, args) })
	if err != nil && !errors.Is(err, rabbitmq.ErrQueueClosed) {
		c.logger.Error(fmt.Sprintf("dead letter consumer %s: %v", c.marketRef, err))
	}
}

func (c *Consumer) Close() error {
	if err := c.queue.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrConsumerClosed, err)
	}
	return nil
}

func (c *Consumer) handle(ctx context.Context, args *rabbitmq.ConsumeArgs) {
	if err := c.repo.InsertDeadLetter(ctx, c.record(args)); err != nil {
		if c.metrics != nil {
			c.metrics.IncDLQPersistFailure()
		}
		c.requeue(ctx, args)
		return
	}
	if err := args.Ack(); err != nil {
		c.logger.Error(fmt.Sprintf("dead letter consumer %s: ack %s: %v", c.marketRef, args.Id(), err))
	}
}

func (c *Consumer) record(args *rabbitmq.ConsumeArgs) repository.InsertDeadLetterParams {
	raw := args.RawMessage()
	verdict := ClassifyRaw(raw, c.constraints)
	params := repository.InsertDeadLetterParams{
		MessageID: args.Id(),
		MarketRef: c.marketRef,
		EventType: verdict.EventType,
		Reason:    string(verdict.Reason),
		Error:     verdict.Error,
		Payload:   payloadJSON(raw),
		DeadAt:    deadAt(args),
	}
	if verdict.OrderID != uuid.Nil {
		id := verdict.OrderID
		params.OrderID = &id
	}
	if !verdict.Dead() {
		params.Reason = string(ReasonPoison)
		params.Error = PoisonErrorUnavailable
		if text, ok := c.poison.Take(args.Id(), verdict.EventType); ok {
			params.Error = text
		}
	}
	return params
}

// requeue puts the delivery back and waits out the backoff, so the next attempt is not immediate.
func (c *Consumer) requeue(ctx context.Context, args *rabbitmq.ConsumeArgs) {
	if err := args.Nack(); err != nil {
		c.logger.Error(fmt.Sprintf("dead letter consumer %s: nack %s: %v", c.marketRef, args.Id(), err))
	}
	select {
	case <-ctx.Done():
	case <-time.After(persistBackoff):
	}
}

// deadAt is the broker's dead-letter time; a message published onto the parking queue by hand
// has none and is dated now.
func deadAt(args *rabbitmq.ConsumeArgs) time.Time {
	if t, ok := args.DeadLetteredAt(); ok {
		return t.UTC()
	}
	return time.Now().UTC()
}
