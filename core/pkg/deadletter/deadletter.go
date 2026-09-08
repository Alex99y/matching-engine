// Package deadletter is core's parking lot for order commands that cannot be processed.
//
// Dead-lettering here is application-driven rather than broker-driven: core publishes the failed
// message to the me.dlx exchange itself and then acks the original, instead of relying on an
// x-dead-letter-exchange argument on the command queues. Two reasons, both specific to this engine:
//
//   - The broker's x-death header records only {queue, reason:"rejected", count, time}. The reason an
//     order actually failed — the constraint it violated, the validation rule it broke — is the whole
//     point of a dead-letter queue for a matching engine, and only core knows it.
//   - Adding the argument to the existing durable command queues would fail with PRECONDITION_FAILED,
//     crash-looping both core and api at boot until every queue was drained and recreated.
//
// The publisher is deliberately synchronous, unlike marketevents: the caller must know whether the
// message reached the parking lot before it decides to ack the original.
package deadletter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/google/uuid"
)

const ExchangeName = "me.dlx"

const queuePrefix = "me.dlq."

func QueueName(marketRef string) string { return queuePrefix + marketRef }

type Reason string

const (
	// ReasonMalformed is an envelope or payload that would not decode.
	ReasonMalformed Reason = "malformed"
	// ReasonInvalid is a well-formed order that failed validation against the market's constraints.
	ReasonInvalid Reason = "invalid"
	// ReasonUnknownType is a well-formed envelope carrying an event type this core does not handle.
	ReasonUnknownType Reason = "unknown_type"
	// ReasonPoison is an order that failed to commit deterministically maxOrderFailures times.
	ReasonPoison Reason = "poison"
)

// Envelope is what lands in the parking lot. Payload holds the original message so an operator can
// replay it by hand after fixing the root cause.
type Envelope struct {
	OrderID   string          `json:"order_id,omitempty"`
	MarketRef string          `json:"market_ref"`
	EventType string          `json:"event_type,omitempty"`
	Reason    Reason          `json:"reason"`
	Error     string          `json:"error,omitempty"`
	Failures  int             `json:"failures,omitempty"`
	DeadAt    time.Time       `json:"dead_at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Publisher struct {
	exchange  *rabbitmq.Exchange
	client    *rabbitmq.RabbitMQClient
	logger    *logger.Logger
	markets   []string
	mu        sync.Mutex
	publishMu sync.Mutex
}

func NewPublisher(client *rabbitmq.RabbitMQClient, log *logger.Logger) (*Publisher, error) {
	if log == nil {
		panic("logger cannot be nil")
	}
	if client == nil {
		panic("rabbitMqClient cannot be nil")
	}
	exchange, err := rabbitmq.NewExchange(client, rabbitmq.ExchangeArgs{
		Name:    ExchangeName,
		Kind:    rabbitmq.ExchangeKindDirect,
		Durable: true,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("dead letter publisher: %w", err)
	}
	return &Publisher{exchange: exchange, client: client, logger: log}, nil
}

// DeclareMarket creates the durable parking-lot queue for one market and
// binds it to me.dlx under the market ref.
func (p *Publisher) DeclareMarket(marketRef string) error {
	if err := p.declare(marketRef); err != nil {
		return err
	}
	p.mu.Lock()
	p.markets = append(p.markets, marketRef)
	p.mu.Unlock()
	return nil
}

func (p *Publisher) declare(marketRef string) error {
	err := rabbitmq.DeclareQueue(p.client, rabbitmq.QueueArgs{
		Name:     QueueName(marketRef),
		Durable:  true,
		Bindings: []rabbitmq.Binding{{Exchange: ExchangeName, RoutingKey: marketRef}},
	})
	if err != nil {
		return fmt.Errorf("dead letter queue %q: %w", QueueName(marketRef), err)
	}
	return nil
}

// Publish parks one envelope. A nil error means the broker accepted it; the caller may then ack the
// original message.
func (p *Publisher) Publish(ctx context.Context, env *Envelope) error {
	if env.DeadAt.IsZero() {
		env.DeadAt = time.Now().UTC()
	}
	if err := normalisePayload(env); err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("dead letter marshal: %w", err)
	}

	p.publishMu.Lock()
	defer p.publishMu.Unlock()

	if err := p.exchange.PublishPersistent(ctx, env.MarketRef, uuid.NewString(), body); err == nil {
		return nil
	}

	if rErr := p.redeclare(); rErr != nil {
		return fmt.Errorf("dead letter publish %q: topology re-declare failed: %w", env.MarketRef, rErr)
	}
	if err := p.exchange.PublishPersistent(ctx, env.MarketRef, uuid.NewString(), body); err != nil {
		return fmt.Errorf("dead letter publish %q: %w", env.MarketRef, err)
	}
	return nil
}

// normalisePayload makes env.Payload safe to marshal.
func normalisePayload(env *Envelope) error {
	if len(env.Payload) == 0 || json.Valid(env.Payload) {
		return nil
	}
	quoted, err := json.Marshal(string(env.Payload))
	if err != nil {
		return fmt.Errorf("dead letter marshal payload: %w", err)
	}
	env.Payload = quoted
	return nil
}

func (p *Publisher) redeclare() error {
	p.mu.Lock()
	markets := make([]string, len(p.markets))
	copy(markets, p.markets)
	p.mu.Unlock()

	for _, ref := range markets {
		if err := p.declare(ref); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) Close() error { return p.exchange.Close() }
