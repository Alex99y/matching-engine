package rabbitmq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/rabbitmq/amqp091-go"
)

type Queue struct {
	client      *RabbitMQClient
	channel     *amqp091.Channel
	queue       *amqp091.Queue
	logger      *logger.Logger
	channelArgs ChannelArgs
	queueArgs   QueueArgs
	mu          sync.RWMutex
}

type ChannelArgs struct {
	PrefetchCount int
	PrefetchSize  int
}

type QueueArgs struct {
	Name       string
	Durable    bool
	AutoDelete bool
	Exclusive  bool
	NoWait     bool
	Args       map[string]any
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
	queue, err := channel.QueueDeclare(
		queueArgs.Name,
		queueArgs.Durable,
		queueArgs.AutoDelete,
		queueArgs.Exclusive,
		queueArgs.NoWait,
		queueArgs.Args,
	)
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

func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.channel.Close()
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
	q.mu.RLock()
	ch := q.channel
	name := q.queue.Name
	q.mu.RUnlock()
	return ch.PublishWithContext(
		ctx,
		"",
		name,
		false,
		false,
		newJSONPublishing(messageId, message, persistent),
	)
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

// reopen closes the dead channel and opens a fresh one against the current connection.
// Called by Consume after an unexpected channel closure.
func (q *Queue) reopen() error {
	q.mu.RLock()
	oldCh := q.channel
	q.mu.RUnlock()
	if oldCh != nil {
		oldCh.Close() // ignore error — channel is likely already dead
	}

	ch, err := q.client.CreateChannel(q.channelArgs.PrefetchCount, q.channelArgs.PrefetchSize)
	if err != nil {
		return fmt.Errorf("rabbitmq reopen channel: %w", err)
	}
	queue, err := ch.QueueDeclare(
		q.queueArgs.Name,
		q.queueArgs.Durable,
		q.queueArgs.AutoDelete,
		q.queueArgs.Exclusive,
		q.queueArgs.NoWait,
		q.queueArgs.Args,
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("rabbitmq reopen queue declare: %w", err)
	}

	q.mu.Lock()
	q.channel = ch
	q.queue = &queue
	q.mu.Unlock()
	return nil
}

// consumeOnce runs the delivery loop for the current channel until it closes or ctx is cancelled.
// Deliveries are processed sequentially to guarantee FIFO ordering within the queue.
func (q *Queue) consumeOnce(ctx context.Context, callback ConsumeCallback) error {
	q.mu.RLock()
	ch := q.channel
	name := q.queue.Name
	q.mu.RUnlock()

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

		if err := q.reopen(); err != nil {
			q.logger.Error(fmt.Sprintf("rabbitmq: channel reopen failed: %v", err))
		}
	}
}
