package orderprocessors

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/core/internal/metrics"
	"github.com/alex99y/matching-engine/core/internal/orderbook"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file is the entry point: what an OrderProcessor is and how to build/start/stop one.
// The consumer boundary is delivery.go, the matcher's select loop is matcher.go, batch
// execution is batch.go, failure recovery is recovery.go, and event-log publishing is
// stream.go.

const (
	// orderChannelBuffer must be >= the RabbitMQ prefetch so the consumer can stage a full
	// pipeline without blocking on the channel. maxBatchSize is the orders-per-transaction
	// cap; prefetch (see order_events_queue) must be >= it for batches to actually fill.
	orderChannelBuffer = 256
	maxBatchSize       = 128
	maxBatchWait       = 5 * time.Millisecond
	rebuildBackoff     = 2 * time.Second
	// transientBackoff paces retries of a batch that failed on infrastructure (DB down,
	// deadlock), so a sick dependency can't spin the matcher.
	transientBackoff = 1 * time.Second
	// poisonBackoff paces re-attempts of a poison candidate during isolation, giving an
	// operator time to react before the candidate is dead-lettered.
	poisonBackoff = 250 * time.Millisecond
	// maxOrderFailures is how many isolation failures an order survives before it is
	// dead-lettered (rejected without requeue).
	maxOrderFailures = 10
	// snapshotInterval / heartbeatInterval drive the event-log stream
	snapshotInterval  = 5 * time.Second
	heartbeatInterval = 2 * time.Second
	// expirySweepInterval bounds how long an order can outlive its ExpiresAt before it's
	// reaped. Fires from the matcher's select loop (with snapshot/heartbeat), so no second
	// goroutine and no concurrency with a real batch.
	expirySweepInterval = 1 * time.Second
)

// eventPublisher is the subset of marketevents.Publisher the processor needs. Declared here (the
// consumer) per the layer-architecture rule; a nil publisher disables event-log emission entirely.
type eventPublisher interface {
	Enqueue(routingKey, messageId string, body []byte) bool
}

// orderEventsQueue is the subset of order_events_queue.OrdersEventsQueue the processor needs.
type orderEventsQueue interface {
	WatchForOrderEvents(ctx context.Context, handler oeq.OrderDeliveryHandler) error
}

// orderRepository is the subset of repository.OrderRepository the processor needs.
// Declared here (the consumer) per the layer-architecture rule.
type orderRepository interface {
	ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error
	LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error)
}

// queuedEvent carries a validated, decoded event together with its broker delivery so
// the matcher can ack/nack it after the batch commits. delivery is nil for a synthetic
// expiry event (see buildExpiryBatch), which has no broker message to ack/nack — the
// matcher's ack/nack helpers must treat that as a no-op.
type queuedEvent struct {
	delivery *oeq.OrderDelivery
	open     *oeq.OpenOrderEvent   // set for an open-order event
	cancel   *oeq.CancelOrderEvent // set for a cancel-order event
	expire   *uuid.UUID            // set for a synthetic TTL-expiry event
}

type OrderProcessor struct {
	logger        *logger.Logger
	market        *repository.Market
	queue         orderEventsQueue
	repo          orderRepository
	constraints   oeq.MarketConstraints
	book          *orderbook.OrderBook // owned and mutated solely by the matcher goroutine
	ordersChannel chan *queuedEvent
	metrics       *metrics.MarketMetrics // per-market pre-bound handles; nil disables recording
	stopMatcher   atomic.Bool
	// Event-log stream (docs/event-log.md). publisher is nil-able (disables emission). epoch is a
	// fresh id per core start; seq is a per-market monotonic counter advanced once per book delta,
	// so the API can detect a gap (missed delta) or restart (changed epoch). Touched only by the
	// matcher goroutine, so no synchronisation.
	publisher eventPublisher
	marketRef string
	epoch     string
	seq       uint64
	// failures counts consecutive isolation failures per order id; accessed only by the
	// matcher goroutine. An order is dead-lettered once it reaches maxOrderFailures.
	failures map[uuid.UUID]int
}

// Start hydrates the book from the DB, launches the matcher goroutine, then blocks on
// the RabbitMQ consumer until ctx is cancelled. Call it in its own goroutine from main.
// Start itself does not return until the matcher has actually exited, so a caller that waits
// for Start (e.g. via a WaitGroup) can safely tear down shared resources (DB, AMQP) once it does.
func (o *OrderProcessor) Start(ctx context.Context) {
	// DB work must outlive ctx cancellation so an in-flight batch can still commit
	// during shutdown; a stranded commit is harmless thanks to idempotent reprocessing.
	dbCtx := context.Background()

	if !o.loadBook(ctx, dbCtx) {
		o.logger.Warn(fmt.Sprintf("order processor %s-%s: shut down before initial hydration",
			o.market.BaseSymbol, o.market.QuoteSymbol))
		return
	}

	matcherDone := make(chan struct{})
	go func() {
		defer close(matcherDone)
		o.matcher(ctx, dbCtx)
	}()

	if err := o.queue.WatchForOrderEvents(ctx, o.handleDelivery); err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s-%s: consumer error: %s",
			o.market.BaseSymbol, o.market.QuoteSymbol, err))
	}
	close(o.ordersChannel)
	<-matcherDone
}

// Stop prevents the consumer from queueing further events to the matcher; in-flight
// events are requeued. Full shutdown still requires cancelling the context passed to Start.
func (o *OrderProcessor) Stop() {
	o.stopMatcher.Store(true)
}

func NewOrderProcessor(
	log *logger.Logger,
	market *repository.Market,
	queue orderEventsQueue,
	repo orderRepository,
	coreMetrics *metrics.CoreMetrics,
	publisher eventPublisher,
	epoch string,
) *OrderProcessor {
	if log == nil {
		panic("logger cannot be nil")
	}
	if market == nil {
		panic("market cannot be nil")
	}
	if queue == nil {
		panic("order events queue cannot be nil")
	}
	if repo == nil {
		panic("order repository cannot be nil")
	}

	marketRef := utils.MergeMarketRef(market.BaseSymbol, market.QuoteSymbol)
	return &OrderProcessor{
		logger:    log,
		market:    market,
		queue:     queue,
		repo:      repo,
		metrics:   coreMetrics.BindMarket(marketRef),
		publisher: publisher,
		marketRef: marketRef,
		epoch:     epoch,
		constraints: oeq.MarketConstraints{
			PriceQuantum:  market.PriceQuantum,
			AmountQuantum: market.AmountQuantum,
			MinOrderSize:  market.MinOrderSize,
			MaxOrderSize:  market.MaxOrderSize,
		},
		ordersChannel: make(chan *queuedEvent, orderChannelBuffer),
		failures:      make(map[uuid.UUID]int),
	}
}
