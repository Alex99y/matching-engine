package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/rabbitmq/amqp091-go"
)

var ErrQueueClosed = errors.New("queue is closed")

type Queue struct {
	client      *RabbitMQClient
	channel     *amqp091.Channel
	queue       *amqp091.Queue
	logger      *logger.Logger
	channelArgs ChannelArgs
	queueArgs   QueueArgs
	closed      bool
	mu          sync.RWMutex
}

type ChannelArgs struct {
	PrefetchCount int
	PrefetchSize  int
}

// Binding attaches the queue to an exchange under a routing key.
type Binding struct {
	Exchange   string
	RoutingKey string
}

type QueueArgs struct {
	Name       string
	Durable    bool
	AutoDelete bool
	Exclusive  bool
	NoWait     bool
	Args       map[string]any
	Bindings   []Binding
}

func NewQueue(
	rabbitmq *RabbitMQClient,
	channelArgs ChannelArgs,
	queueArgs QueueArgs,
	logger *logger.Logger,
) (*Queue, error) {
	channel, err := rabbitmq.CreateChannel(
		channelArgs.PrefetchCount,
		channelArgs.PrefetchSize,
	)
	if err != nil {
		return nil, err
	}
	queue, err := declareAndBind(channel, queueArgs)
	if err != nil {
		channel.Close()
		return nil, err
	}
	return &Queue{
		client:      rabbitmq,
		channel:     channel,
		queue:       &queue,
		logger:      logger,
		channelArgs: channelArgs,
		queueArgs:   queueArgs,
	}, nil
}

// DeclareQueue declares a queue and its bindings on a throwaway channel, for topology a process
// creates but never itself consumes from or publishes directly to. Unlike NewQueue it holds no
// channel open afterwards, so it is safe to call repeatedly to re-assert topology after a reconnect.
func DeclareQueue(client *RabbitMQClient, args QueueArgs) error {
	ch, err := client.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if _, err := declareAndBind(ch, args); err != nil {
		return err
	}
	return nil
}

func declareAndBind(ch *amqp091.Channel, args QueueArgs) (amqp091.Queue, error) {
	queue, err := ch.QueueDeclare(
		args.Name,
		args.Durable,
		args.AutoDelete,
		args.Exclusive,
		args.NoWait,
		args.Args,
	)
	if err != nil {
		return amqp091.Queue{}, err
	}
	for _, b := range args.Bindings {
		if err := ch.QueueBind(queue.Name, b.RoutingKey, b.Exchange, args.NoWait, nil); err != nil {
			return amqp091.Queue{}, fmt.Errorf("rabbitmq bind %q to %q: %w", queue.Name, b.Exchange, err)
		}
	}
	return queue, nil
}

func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	return q.channel.Close()
}

func (q *Queue) snapshot() (*amqp091.Channel, string) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.channel, q.queue.Name
}

func (q *Queue) Name() string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.queue.Name
}

func (q *Queue) Publish(
	ctx context.Context,
	messageId string,
	message []byte,
	persistent bool,
) error {
	publishing := newJSONPublishing(messageId, message, persistent)

	ch, name := q.snapshot()
	err := ch.PublishWithContext(ctx, "", name, false, false, publishing)
	if !isChannelClosed(err) {
		return err
	}

	// Re-open the channel and try to publish the event again
	if err := q.reopen(ch); err != nil {
		return fmt.Errorf("rabbitmq publish: channel closed and reopen failed: %w", err)
	}

	ch, name = q.snapshot()
	return ch.PublishWithContext(ctx, "", name, false, false, publishing)
}

func isChannelClosed(err error) bool {
	return errors.Is(err, amqp091.ErrClosed)
}

func newJSONPublishing(messageId string, message []byte, persistent bool) amqp091.Publishing {
	deliveryMode := amqp091.Transient
	if persistent {
		deliveryMode = amqp091.Persistent
	}
	return amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: deliveryMode,
		MessageId:    messageId,
		Body:         message,
	}
}

type MessageMetadata struct {
	messageType     string
	messageEncoding string
	timestamp       time.Time
	expiration      string
}

func (m MessageMetadata) GetMsgType() string      { return m.messageType }
func (m MessageMetadata) GetMsgEncoding() string  { return m.messageEncoding }
func (m MessageMetadata) GetTimestamp() time.Time { return m.timestamp }
func (m MessageMetadata) GetExpiration() string   { return m.expiration }

type ConsumeArgs struct {
	id       string
	message  []byte
	metadata MessageMetadata
	ack      func() error
	nack     func() error
	reject   func() error
}

func (a *ConsumeArgs) Id() string                          { return a.id }
func (a *ConsumeArgs) RawMessage() []byte                  { return a.message }
func (a *ConsumeArgs) Ack() error                          { return a.ack() }
func (a *ConsumeArgs) Nack() error                         { return a.nack() }
func (a *ConsumeArgs) Reject() error                       { return a.reject() }
func (a *ConsumeArgs) GetMessageMetadata() MessageMetadata { return a.metadata }

type ConsumeCallback func(*ConsumeArgs)

func (q *Queue) handleDelivery(delivery amqp091.Delivery, callback ConsumeCallback) {
	args := &ConsumeArgs{
		id:      delivery.MessageId,
		message: delivery.Body,
		metadata: MessageMetadata{
			messageType:     delivery.Type,
			messageEncoding: delivery.ContentEncoding,
			timestamp:       delivery.Timestamp,
			expiration:      delivery.Expiration,
		},
		ack:    func() error { return delivery.Ack(false) },
		nack:   func() error { return delivery.Nack(false, true) },
		reject: func() error { return delivery.Reject(false) },
	}
	callback(args)
	q.logger.Debug(fmt.Sprintf("message %s processed", delivery.MessageId))
}

func (q *Queue) reopen(stale *amqp091.Channel) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}
	if stale != nil && q.channel != stale {
		return nil
	}
	if q.channel != nil {
		q.channel.Close() // ignore error — channel is likely already dead
	}

	ch, err := q.client.CreateChannel(q.channelArgs.PrefetchCount, q.channelArgs.PrefetchSize)
	if err != nil {
		return fmt.Errorf("rabbitmq reopen channel: %w", err)
	}
	queue, err := declareAndBind(ch, q.queueArgs)
	if err != nil {
		ch.Close()
		return fmt.Errorf("rabbitmq reopen queue declare: %w", err)
	}

	q.channel = ch
	q.queue = &queue
	return nil
}

// consumeOnce runs the delivery loop for the current channel until it closes or ctx is cancelled.
// Deliveries are processed sequentially to guarantee FIFO ordering within the queue.
func (q *Queue) consumeOnce(ctx context.Context, callback ConsumeCallback) error {
	ch, name := q.snapshot()

	deliveries, err := ch.ConsumeWithContext(
		ctx,
		name,
		"",    // consumer tag — broker generates one
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	for delivery := range deliveries {
		q.handleDelivery(delivery, callback)
	}

	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("channel closed unexpectedly")
}

// Consume blocks until ctx is cancelled, processing each delivery sequentially.
// On unexpected channel closure it reopens the channel and resumes automatically.
func (q *Queue) Consume(ctx context.Context, callback ConsumeCallback) error {
	for {
		err := q.consumeOnce(ctx, callback)
		if ctx.Err() != nil {
			return nil
		}
		q.logger.Error(fmt.Sprintf("rabbitmq: consumer channel closed (%v) — reopening", err))

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}

		if err := q.reopen(nil); err != nil {
			if errors.Is(err, ErrQueueClosed) {
				return nil
			}
			q.logger.Error(fmt.Sprintf("rabbitmq: channel reopen failed: %v", err))
		}
	}
}
