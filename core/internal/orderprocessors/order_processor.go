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
	"github.com/alex99y/matching-engine/core/pkg/deadletter"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file is the entry point: what an OrderProcessor is, how to build/start/stop one, and the
// consumer-goroutine boundary (handleDelivery). The matcher's select loop is matcher.go, batch
// execution is batch.go, failure recovery is recovery.go, dead-lettering is deadletter.go, and
// event-log publishing is stream.go.

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
	EmitCancelOrder(ctx context.Context, orderID uuid.UUID) error
	Pause()
	Resume()
	IsPaused() bool
}

// orderRepository is the subset of repository.OrderRepository the processor needs.
// Declared here (the consumer) per the layer-architecture rule.
type orderRepository interface {
	ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error
	LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error)
	LoadPendingOrders(ctx context.Context, marketID int) ([]repository.PendingOrderHydration, error)
	LoadLastPrice(ctx context.Context, marketID int) (price uint64, ok bool, err error)
}

// queuedEvent carries a validated, decoded event together with its broker delivery so
// the matcher can ack/nack it after the batch commits. Every queuedEvent originates from a broker
// message, so delivery is always set — expiry is not an event but a sweep the match callback runs
// (see buildMatch).
type queuedEvent struct {
	delivery *oeq.OrderDelivery
	open     *oeq.OpenOrderEvent   // set for an open-order event
	cancel   *oeq.CancelOrderEvent // set for a cancel-order event
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
	// Event-log stream (docs/event-log.md). publisher is nil-able (disables emission). epoch is a
	// fresh id per core start; seq is a per-market monotonic counter advanced once per book delta,
	// so the API can detect a gap (missed delta) or restart (changed epoch)
	publisher eventPublisher
	marketRef string
	epoch     string
	seq       atomic.Uint64
	// failures counts consecutive isolation failures per order id; accessed only by the
	// matcher goroutine. An order is dead-lettered once it reaches maxOrderFailures.
	failures map[uuid.UUID]int
	poison   poisonRecorder
	// sweepInterval and poisonDelay stand in for expirySweepInterval and poisonBackoff when
	// non-zero. Nothing in production sets them; tests do, so they need not outwait real seconds.
	sweepInterval time.Duration
	poisonDelay   time.Duration
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

// handleDelivery runs on the consumer goroutine: it rejects the deliveries that can never be
// processed (see deadletter.go) and forwards the rest to the matcher. It never touches the book,
// so there is no race with the matcher goroutine.
func (o *OrderProcessor) handleDelivery(d *oeq.OrderDelivery) {
	verdict := deadletter.Classify(d.Event, o.constraints)
	if verdict.Dead() {
		o.deadLetter(d, verdict.Reason)
		if verdict.Open != nil {
			o.notifyDeadLettered(verdict.Open.UserID, verdict.Open.OrderID, orderbook.StatusDeadLettered)
		}
		return
	}
	if verdict.Open != nil {
		o.metrics.IncReceived()
	}
	o.ordersChannel <- &queuedEvent{delivery: d, open: verdict.Open, cancel: verdict.Cancel}
}

// Pause halts trading on this market: the consumer stops, and commands accumulate in the broker
// queue until Resume drains them. The book is untouched and the matcher keeps running, so resting
// orders still expire and release their funds while the market is down.
func (o *OrderProcessor) Pause() {
	o.queue.Pause()
	o.metrics.SetPaused(true)
}

func (o *OrderProcessor) Resume() {
	o.queue.Resume()
	o.metrics.SetPaused(false)
}

func (o *OrderProcessor) IsPaused() bool { return o.queue.IsPaused() }

// EmitCancel queues a cancel for one of this market's orders on behalf of the admin API.
func (o *OrderProcessor) EmitCancel(ctx context.Context, orderID uuid.UUID) error {
	return o.queue.EmitCancelOrder(ctx, orderID)
}

func NewOrderProcessor(
	log *logger.Logger,
	market *repository.Market,
	queue orderEventsQueue,
	repo orderRepository,
	coreMetrics *metrics.CoreMetrics,
	publisher eventPublisher,
	poison poisonRecorder,
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
	if poison == nil {
		panic("poison recorder cannot be nil")
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
			BaseScale:     market.BaseScale,
		},
		ordersChannel: make(chan *queuedEvent, orderChannelBuffer),
		failures:      make(map[uuid.UUID]int),
		poison:        poison,
	}
}
