// Package marketevents is core's transport for the live event-log stream (see docs/event-log.md):
// a single async publisher that fans serialized market-data events out to the me.events topic
// exchange. It is deliberately best-effort — it never blocks the matcher and drops events under
// back-pressure, leaving consumers to re-synchronise from the next snapshot via the sequence gap.
package marketevents

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
)

// outboundBuffer is generous on purpose: events are tiny (a few hundred bytes) and the buffer only
// needs to ride over a brief publish stall before drop-on-full kicks in. One buffer is shared by
// every market's matcher (many producers, one publisher goroutine).
const outboundBuffer = 4096

// outbound is one already-serialized event awaiting publication. The matcher does all the
// envelope/serialization work; the publisher goroutine only moves bytes to the broker.
type outbound struct {
	routingKey string
	messageId  string
	body       []byte
}

// Metrics records publisher-side observability. It is the minimal surface the publisher needs
// (interface in the consumer); a nil Metrics disables recording. Buffer-full drops are recorded by
// the caller (which has the market context); broker publish failures are recorded here.
type Metrics interface {
	IncPublishError()
}

// Publisher owns the me.events exchange and the single goroutine that publishes to it. Because that
// goroutine is the only thing that ever touches the underlying AMQP channel (which is not safe for
// concurrent use), every market can enqueue concurrently without a lock.
type Publisher struct {
	exchange *rabbitmq.Exchange
	logger   *logger.Logger
	metrics  Metrics
	ch       chan outbound
	dropped  atomic.Uint64
}

func NewPublisher(client *rabbitmq.RabbitMQClient, log *logger.Logger, metrics Metrics) (*Publisher, error) {
	if log == nil {
		panic("logger cannot be nil")
	}
	if client == nil {
		panic("rabbitMqClient cannot be nil")
	}
	exchange, err := rabbitmq.NewExchange(client, rabbitmq.ExchangeArgs{
		Name:    marketdata.ExchangeName,
		Kind:    rabbitmq.ExchangeKindTopic,
		Durable: true, // persists only the routing rule; messages are still transient
	}, log)
	if err != nil {
		return nil, fmt.Errorf("market events publisher: %w", err)
	}
	return &Publisher{
		exchange: exchange,
		logger:   log,
		metrics:  metrics,
		ch:       make(chan outbound, outboundBuffer),
	}, nil
}

// Enqueue hands a pre-serialized event to the async publisher. It NEVER blocks: if the buffer is
// full (broker slow or down) the event is dropped and counted, and consumers re-synchronise from
// the next snapshot via the sequence gap. Called from the matcher goroutine, off the hot path
// (after the batch has committed). Returns false when the event was dropped.
func (p *Publisher) Enqueue(routingKey, messageId string, body []byte) bool {
	select {
	case p.ch <- outbound{routingKey: routingKey, messageId: messageId, body: body}:
		return true
	default:
		p.dropped.Add(1)
		return false
	}
}

// Run drains the buffer and publishes until ctx is cancelled. Run as a single goroutine from main.
func (p *Publisher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-p.ch:
			// Best-effort: a publish failure (after the exchange's own reopen-retry) is logged and
			// the event dropped. Blocking matching on the broker is never acceptable here.
			if err := p.exchange.Publish(ctx, o.routingKey, o.messageId, o.body); err != nil {
				p.dropped.Add(1)
				if p.metrics != nil {
					p.metrics.IncPublishError()
				}
				p.logger.Error(fmt.Sprintf("market events publisher: dropped %q: %v", o.routingKey, err))
			}
		}
	}
}

func (p *Publisher) Close() error { return p.exchange.Close() }

// Dropped is the running count of events dropped on a full buffer or a failed publish.
func (p *Publisher) Dropped() uint64 { return p.dropped.Load() }
