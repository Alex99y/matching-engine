// Package deadletter is core's record of order commands that could never be processed.
//
// Dead-lettering is broker-driven: every command queue is declared with an x-dead-letter-exchange
// of me.dlx, so when the matcher rejects a message the broker moves it — atomically with the
// reject — to the market's parking queue me.dlq.<market>. The Consumer in this package drains
// that queue into the dead_letters table, which is what operators read.
//
// The broker records only that a message was rejected, not why. The why is recomputed instead of
// remembered: classification is a pure function of the payload and the market's constraints
// (Classify), so a malformed, unknown-type or invalid command is re-derived exactly, at any time,
// after any restart. A payload that classifies as processable can be on the parking queue for
// one reason only — it failed to commit deterministically too many times — so it is poison. The
// one value that is not a function of the payload is the poison error text (which database
// constraint fired); PoisonErrors carries it from the matcher to the consumer in-process, best
// effort, and a restart between the reject and the consume leaves a placeholder on that row.
package deadletter

import (
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
)

const ExchangeName = oeq.DeadLetterExchange

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

// PoisonErrorUnavailable stands in for a poison error text the consumer could not recover: the
// matcher that rejected the message is not the process consuming it, or it restarted in between.
const PoisonErrorUnavailable = "see core log at dead_at"

// DeclareTopology creates the parking queue for one market, bound to me.dlx under the market
// ref. The exchange is declared with it. Safe to call repeatedly.
func DeclareTopology(client *rabbitmq.RabbitMQClient, marketRef string) error {
	ch, err := client.Channel()
	if err != nil {
		return fmt.Errorf("dead letter topology %q: %w", marketRef, err)
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare(ExchangeName, string(rabbitmq.ExchangeKindDirect), true, false, false, false, nil); err != nil {
		return fmt.Errorf("dead letter exchange: %w", err)
	}
	err = rabbitmq.DeclareQueue(client, rabbitmq.QueueArgs{
		Name:     QueueName(marketRef),
		Durable:  true,
		Bindings: []rabbitmq.Binding{{Exchange: ExchangeName, RoutingKey: marketRef}},
	})
	if err != nil {
		return fmt.Errorf("dead letter queue %q: %w", QueueName(marketRef), err)
	}
	return nil
}

// poisonErrorTTL bounds how long a recorded error waits for its consumer. The broker delivers a
// rejected message within milliseconds; anything older belongs to a message that will never
// arrive here.
const poisonErrorTTL = time.Minute

type poisonEntry struct {
	text string
	at   time.Time
}

// PoisonErrors hands a poison error text from the matcher goroutine that rejects a message to the
// consumer goroutine that records it. Keyed by message id and event type together, because the
// API uses an order's id as the message id of both its open and its cancel.
type PoisonErrors struct {
	mu      sync.Mutex
	entries map[string]poisonEntry
	now     func() time.Time
}

func NewPoisonErrors() *PoisonErrors {
	return &PoisonErrors{entries: make(map[string]poisonEntry), now: time.Now}
}

func (p *PoisonErrors) Record(messageID, eventType, text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for k, e := range p.entries {
		if now.Sub(e.at) > poisonErrorTTL {
			delete(p.entries, k)
		}
	}
	p.entries[poisonKey(messageID, eventType)] = poisonEntry{text: text, at: now}
}

func (p *PoisonErrors) Take(messageID, eventType string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := poisonKey(messageID, eventType)
	e, ok := p.entries[key]
	if !ok {
		return "", false
	}
	delete(p.entries, key)
	return e.text, true
}

func poisonKey(messageID, eventType string) string { return messageID + "|" + eventType }
