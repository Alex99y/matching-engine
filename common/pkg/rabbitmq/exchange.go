package rabbitmq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/rabbitmq/amqp091-go"
)

// ExchangeKind is the RabbitMQ exchange type.
type ExchangeKind string

const (
	ExchangeKindTopic  ExchangeKind = "topic"
	ExchangeKindFanout ExchangeKind = "fanout"
	ExchangeKindDirect ExchangeKind = "direct"
)

// ExchangeArgs declares a RabbitMQ exchange. For the live event fan-out use a durable topic
// exchange: durability is cheap (it persists only the routing rule, not messages) and avoids
// "exchange not found" races, while the messages themselves are published transiently and the
// subscriber queues are non-durable — so nothing is ever stored.
type ExchangeArgs struct {
	Name       string
	Kind       ExchangeKind
	Durable    bool
	AutoDelete bool
	Internal   bool
	NoWait     bool
	Args       map[string]any
}

// Exchange publishes messages that the broker fans out to every bound queue. Unlike Queue (a
// shared work queue where each message is delivered to exactly one consumer), every subscriber
// bound to this exchange receives its own copy — used for best-effort live event broadcast.
// Messages are published transient (non-persistent): if no queue is bound they are dropped, and
// they are never written to disk.
type Exchange struct {
	client  *RabbitMQClient
	logger  *logger.Logger
	args    ExchangeArgs
	channel *amqp091.Channel
	mu      sync.RWMutex
}

func NewExchange(client *RabbitMQClient, args ExchangeArgs, logger *logger.Logger) (*Exchange, error) {
	e := &Exchange{client: client, logger: logger, args: args}
	if err := e.open(); err != nil {
		return nil, err
	}
	return e, nil
}

// open acquires a fresh channel and (re)declares the exchange, closing any previous channel.
func (e *Exchange) open() error {
	e.mu.RLock()
	old := e.channel
	e.mu.RUnlock()
	if old != nil {
		old.Close() // ignore error — likely already dead
	}

	ch, err := e.client.Channel()
	if err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(
		e.args.Name, string(e.args.Kind), e.args.Durable, e.args.AutoDelete,
		e.args.Internal, e.args.NoWait, e.args.Args,
	); err != nil {
		ch.Close()
		return fmt.Errorf("rabbitmq exchange declare %q: %w", e.args.Name, err)
	}

	e.mu.Lock()
	e.channel = ch
	e.mu.Unlock()
	return nil
}

// Publish sends body to the exchange under routingKey as a transient message. On a dead channel
// (e.g. after a connection blip) it reopens once and retries; a second failure is returned so the
// caller can decide (for the event stream: drop and let consumers re-snapshot — never block).
func (e *Exchange) Publish(ctx context.Context, routingKey, messageId string, body []byte) error {
	if err := e.publish(ctx, routingKey, messageId, body); err != nil {
		if rErr := e.open(); rErr != nil {
			return fmt.Errorf("exchange publish %q: %w (reopen failed: %v)", routingKey, err, rErr)
		}
		return e.publish(ctx, routingKey, messageId, body)
	}
	return nil
}

func (e *Exchange) publish(ctx context.Context, routingKey, messageId string, body []byte) error {
	e.mu.RLock()
	ch := e.channel
	name := e.args.Name
	e.mu.RUnlock()
	return ch.PublishWithContext(
		ctx,
		name,
		routingKey,
		false, // mandatory
		false, // immediate
		newJSONPublishing(messageId, body, false), // transient
	)
}

func (e *Exchange) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.channel == nil {
		return nil
	}
	return e.channel.Close()
}

// SubscriberArgs configures a transient fan-out subscriber: a server-named, exclusive,
// auto-delete, non-durable queue bound to Exchange with the given routing-key Patterns. The queue
// and anything in it vanish the moment this process disconnects — nothing is persisted, and
// messages that arrive while disconnected are lost by design (the consumer re-synchronises from a
// snapshot). ExchangeKind must match the publisher's declaration. Patterns are the initial
// bindings; more can be added/removed at runtime with BindPattern/UnbindPattern.
type SubscriberArgs struct {
	Exchange     string
	ExchangeKind ExchangeKind
	Patterns     []string
}

type Subscriber struct {
	client  *RabbitMQClient
	logger  *logger.Logger
	args    SubscriberArgs
	channel *amqp091.Channel
	queue   *amqp091.Queue
	// patterns is the set of active bindings (initial + dynamically added). It is the source of
	// truth re-applied on every (re)declare, so bindings survive a reconnect.
	patterns map[string]struct{}
	mu       sync.Mutex
}

func NewSubscriber(client *RabbitMQClient, args SubscriberArgs, logger *logger.Logger) (*Subscriber, error) {
	s := &Subscriber{
		client:   client,
		logger:   logger,
		args:     args,
		patterns: make(map[string]struct{}, len(args.Patterns)),
	}
	for _, p := range args.Patterns {
		s.patterns[p] = struct{}{}
	}
	if err := s.declare(); err != nil {
		return nil, err
	}
	return s, nil
}

// declare acquires a fresh channel, ensures the exchange exists, declares a new transient queue,
// and re-binds every active pattern. Closing any previous channel auto-deletes the old queue. Holds
// the lock for the whole operation so it cannot interleave with BindPattern/UnbindPattern — the
// control-plane I/O is brief and off the per-message path.
func (s *Subscriber) declare() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.channel != nil {
		s.channel.Close()
	}

	ch, err := s.client.Channel()
	if err != nil {
		return err
	}
	// Idempotent — matches the publisher's declaration so binding can never race ahead of it.
	if err := ch.ExchangeDeclare(s.args.Exchange, string(s.args.ExchangeKind), true, false, false, false, nil); err != nil {
		ch.Close()
		return fmt.Errorf("rabbitmq subscriber exchange declare %q: %w", s.args.Exchange, err)
	}
	// Transient per-instance queue: server-named, non-durable, auto-delete, exclusive.
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		ch.Close()
		return fmt.Errorf("rabbitmq subscriber queue declare: %w", err)
	}
	for pattern := range s.patterns {
		if err := ch.QueueBind(queue.Name, pattern, s.args.Exchange, false, nil); err != nil {
			ch.Close()
			return fmt.Errorf("rabbitmq subscriber bind %q: %w", pattern, err)
		}
	}

	s.channel = ch
	s.queue = &queue
	return nil
}

// ExchangeDelivery is one fanned-out message. RoutingKey lets the handler tell event types apart
// without parsing the body.
type ExchangeDelivery struct {
	RoutingKey string
	MessageId  string
	Body       []byte
}

type ExchangeHandler func(ExchangeDelivery)

// Subscribe consumes the bound stream until ctx is cancelled. Deliveries are auto-acked — the
// stream is ephemeral, there is no redelivery. On channel closure it re-declares the queue,
// re-binds, and resumes; messages missed while disconnected are gone by design.
func (s *Subscriber) Subscribe(ctx context.Context, handler ExchangeHandler) error {
	for {
		err := s.consumeOnce(ctx, handler)
		if ctx.Err() != nil {
			return nil
		}
		s.logger.Error(fmt.Sprintf("rabbitmq subscriber: channel closed (%v) — re-declaring", err))

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
		if err := s.declare(); err != nil {
			s.logger.Error(fmt.Sprintf("rabbitmq subscriber: re-declare failed: %v", err))
		}
	}
}

func (s *Subscriber) consumeOnce(ctx context.Context, handler ExchangeHandler) error {
	s.mu.Lock()
	ch := s.channel
	name := s.queue.Name
	s.mu.Unlock()

	deliveries, err := ch.ConsumeWithContext(
		ctx,
		name,
		"",    // consumer tag
		true,  // auto-ack — ephemeral stream, no redelivery
		true,  // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	for delivery := range deliveries {
		handler(ExchangeDelivery{
			RoutingKey: delivery.RoutingKey,
			MessageId:  delivery.MessageId,
			Body:       delivery.Body,
		})
	}

	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("channel closed unexpectedly")
}

func (s *Subscriber) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.channel == nil {
		return nil
	}
	return s.channel.Close()
}

// BindPattern adds a routing-key binding at runtime (e.g. a per-user private key when that user
// connects). The pattern is recorded so it is re-applied on any later reconnect. Safe to call while
// Subscribe is running. Binding the same pattern twice is harmless (idempotent).
func (s *Subscriber) BindPattern(pattern string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns[pattern] = struct{}{}
	if s.channel == nil {
		return nil // not yet declared; the next declare will bind it
	}
	if err := s.channel.QueueBind(s.queue.Name, pattern, s.args.Exchange, false, nil); err != nil {
		return fmt.Errorf("rabbitmq subscriber bind %q: %w", pattern, err)
	}
	return nil
}

// UnbindPattern removes a runtime binding (e.g. when a user disconnects). It is dropped from the
// recorded set so a later reconnect will not re-add it.
func (s *Subscriber) UnbindPattern(pattern string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.patterns, pattern)
	if s.channel == nil {
		return nil
	}
	if err := s.channel.QueueUnbind(s.queue.Name, pattern, s.args.Exchange, nil); err != nil {
		return fmt.Errorf("rabbitmq subscriber unbind %q: %w", pattern, err)
	}
	return nil
}
