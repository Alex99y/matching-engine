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
	channel *amqp091.Channel
	queue   *amqp091.Queue
	logger  *logger.Logger
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
		channel: channel,
		queue:   &queue,
		logger:  logger,
	}, nil
}

func (q *Queue) Close() error {
	return q.channel.Close()
}

func (q *Queue) Name() string {
	return q.queue.Name
}

func (q *Queue) Publish(ctx context.Context, message []byte, persistent bool) error {
	return q.channel.PublishWithContext(
		ctx,
		"",
		q.queue.Name,
		false, // mandatory
		false, // immediate — not supported by RabbitMQ
		newJSONPublishing(message, persistent),
	)
}

func newJSONPublishing(message []byte, persistent bool) amqp091.Publishing {
	deliveryMode := amqp091.Transient
	if persistent {
		deliveryMode = amqp091.Persistent
	}
	return amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: deliveryMode,
		Body:         message,
	}
}

type MessageMetadata struct {
	messageType     string
	messageEncoding string
	timestamp       time.Time
	expiration      string
}

func (m *MessageMetadata) GetMsgType() string      { return m.messageType }
func (m *MessageMetadata) GetMsgEncoding() string  { return m.messageEncoding }
func (m *MessageMetadata) GetTimestamp() time.Time { return m.timestamp }
func (m *MessageMetadata) GetExpiration() string   { return m.expiration }

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

// Consume blocks until the context is cancelled or the channel closes unexpectedly.
// It waits for all in-flight handlers to finish before returning.
// Returns nil on clean context cancellation, error if the channel closed unexpectedly.
func (q *Queue) Consume(ctx context.Context, callback ConsumeCallback) error {
	deliveries, err := q.channel.ConsumeWithContext(
		ctx,
		q.queue.Name,
		"",
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	for delivery := range deliveries {
		wg.Add(1)
		go func(d amqp091.Delivery) {
			defer wg.Done()
			q.handleDelivery(d, callback)
		}(delivery)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("rabbitmq: consumer channel closed unexpectedly")
}
