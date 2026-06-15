package orderprocessors

import (
	"context"
	"errors"
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
)

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
// the matcher can ack/nack it after the batch commits.
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
	stopMatcher   atomic.Bool
	// failures counts consecutive isolation failures per order id; accessed only by the
	// matcher goroutine. An order is dead-lettered once it reaches maxOrderFailures.
	failures map[uuid.UUID]int
}

// Start hydrates the book from the DB, launches the matcher goroutine, then blocks on
// the RabbitMQ consumer until ctx is cancelled. Call it in its own goroutine from main.
// When the consumer exits it closes ordersChannel so the matcher drains and exits cleanly.
func (o *OrderProcessor) Start(ctx context.Context) {
	// DB work must outlive ctx cancellation so an in-flight batch can still commit
	// during shutdown; a stranded commit is harmless thanks to idempotent reprocessing.
	dbCtx := context.Background()

	if !o.loadBook(ctx, dbCtx) {
		o.logger.Warn(fmt.Sprintf("order processor %s-%s: shut down before initial hydration",
			o.market.BaseSymbol, o.market.QuoteSymbol))
		return
	}

	go o.matcher(ctx, dbCtx)

	if err := o.queue.WatchForOrderEvents(ctx, o.handleDelivery); err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s-%s: consumer error: %s",
			o.market.BaseSymbol, o.market.QuoteSymbol, err))
	}
	close(o.ordersChannel)
}

// Stop prevents the consumer from queueing further events to the matcher; in-flight
// events are requeued. Full shutdown still requires cancelling the context passed to Start.
func (o *OrderProcessor) Stop() {
	o.stopMatcher.Store(true)
}

// handleDelivery runs on the consumer goroutine. It decodes and validates each
// delivery, dropping (acking) malformed or invalid ones, and forwards the rest to the
// matcher. It never touches the book, so there is no race with the matcher goroutine.
func (o *OrderProcessor) handleDelivery(d *oeq.OrderDelivery) {
	qe, ok := o.classify(d)
	if !ok {
		// Malformed/invalid/unknown — drop it. It has no DB effect, so acking now
		// (independently of any batch) is safe and avoids an infinite requeue.
		if err := d.Ack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: ack of dropped message failed: %s", err))
		}
		return
	}
	if o.stopMatcher.Load() {
		if err := d.Nack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: nack while stopping failed: %s", err))
		}
		return
	}
	if qe.open != nil {
		o.metrics.IncReceived()
	}
	o.ordersChannel <- qe
}

func (o *OrderProcessor) classify(d *oeq.OrderDelivery) (*queuedEvent, bool) {
	switch d.Event.Type {
	case oeq.EventTypeOpenOrder:
		open, err := d.Event.DecodeOpenOrder()
		if err != nil {
			o.logger.Warn(fmt.Sprintf("order processor: malformed open_order payload: %v", err))
			return nil, false
		}
		if err := oeq.ValidateOrderEvent(open, o.constraints); err != nil {
			o.logger.Warn(fmt.Sprintf("order processor: invalid order from publisher: %s", err))
			return nil, false
		}
		return &queuedEvent{delivery: d, open: open}, true

	case oeq.EventTypeCancelOrder:
		cancel, err := d.Event.DecodeCancelOrder()
		if err != nil {
			o.logger.Warn(fmt.Sprintf("order processor: malformed cancel_order payload: %v", err))
			return nil, false
		}
		return &queuedEvent{delivery: d, cancel: cancel}, true

	default:
		o.logger.Warn(fmt.Sprintf("order processor: unknown event type %q — dropping", d.Event.Type))
		return nil, false
	}
}

// matcher is the single writer for this market. It drains micro-batches and processes
// each in one transaction, acking after commit or rebuilding the book on failure.
func (o *OrderProcessor) matcher(shutdownCtx, dbCtx context.Context) {
	for {
		batch, open := o.drain()
		if !open {
			return // channel closed and drained
		}
		if !o.runBatch(shutdownCtx, dbCtx, batch) {
			return // shutdown requested during recovery
		}
	}
}

// drain blocks for the first event, then collects more without blocking until the
// batch is full or maxBatchWait elapses. Returns ok=false once the channel is closed
// and empty, so the matcher can exit.
func (o *OrderProcessor) drain() ([]*queuedEvent, bool) {
	first, ok := <-o.ordersChannel
	if !ok {
		return nil, false
	}
	batch := make([]*queuedEvent, 0, maxBatchSize)
	batch = append(batch, first)

	timer := time.NewTimer(maxBatchWait)
	defer timer.Stop()
	for len(batch) < maxBatchSize {
		select {
		case qe, ok := <-o.ordersChannel:
			if !ok {
				return batch, true // channel closed mid-drain; process what we have
			}
			batch = append(batch, qe)
		case <-timer.C:
			return batch, true
		}
	}
	return batch, true
}

// buildIncoming maps the batch's open orders to their persistence + reservation params.
func (o *OrderProcessor) buildIncoming(batch []*queuedEvent) []repository.IncomingOrder {
	incoming := make([]repository.IncomingOrder, 0, len(batch))
	for _, qe := range batch {
		if qe.open == nil {
			continue
		}
		insert := orderbook.DeriveInsertParams(qe.open, o.market)
		incoming = append(incoming, repository.IncomingOrder{
			Insert: insert,
			Reserve: repository.ReserveRequest{
				// The `have` amount is exactly what must be blocked.
				InstrumentID: insert.HaveInstrumentID,
				Amount:       reserveAmount(insert.HaveQuantity),
			},
		})
	}
	return incoming
}

// buildMatch returns the in-memory matching callback for a batch. It runs under
// ProcessBatch's transaction, after funds are reserved, replaying the batch in arrival
// order so cancels and opens interleave with strict FIFO priority; only funded opens
// reach the book.
func (o *OrderProcessor) buildMatch(batch []*queuedEvent) repository.MatchFunc {
	return func(fundedOrderIDs []uuid.UUID) (*repository.BatchResult, error) {
		funded := make(map[uuid.UUID]struct{}, len(fundedOrderIDs))
		for _, id := range fundedOrderIDs {
			funded[id] = struct{}{}
		}
		result := repository.NewBatchResult()
		for _, qe := range batch {
			switch {
			case qe.open != nil:
				if _, ok := funded[qe.open.OrderID]; ok {
					o.book.MatchOrder(qe.open, result)
				}
			case qe.cancel != nil:
				o.book.CancelOrder(qe.cancel, result)
			}
		}
		return result, nil
	}
}

// runBatch processes one micro-batch in a single transaction. On a transient failure it
// rebuilds the book, requeues the batch, and backs off. On a deterministic data error it
// isolates the poison order (committing the healthy ones). Returns false only when
// shutdown is requested mid-recovery.
func (o *OrderProcessor) runBatch(shutdownCtx, dbCtx context.Context, batch []*queuedEvent) bool {
	result, rejected, elapsed, err := o.processBatchCaptured(dbCtx, batch)
	if err == nil {
		o.metrics.ObserveBatch(len(batch), elapsed)
		o.metrics.IncBatch(metrics.BatchCommitted)
		o.recordCommitted(result, rejected)
		o.ackBatch(batch)
		return true
	}

	o.logger.Error(fmt.Sprintf("order processor %s-%s: batch failed: %s",
		o.market.BaseSymbol, o.market.QuoteSymbol, err))
	// The match callback mutated the book before the rollback, so it is dirty: rebuild
	// from the last committed DB state before deciding what to do with the messages.
	o.metrics.IncRebuild()
	if !o.loadBook(shutdownCtx, dbCtx) {
		o.nackBatch(batch)
		return false
	}

	if errors.Is(err, repository.ErrPoison) {
		// At least one order fails deterministically — isolate it so the rest commit.
		o.metrics.IncBatch(metrics.BatchPoisonIsolated)
		o.metrics.IncPoison()
		return o.isolate(shutdownCtx, dbCtx, batch)
	}

	// Transient infrastructure failure: requeue the whole batch and back off so a sick
	// dependency does not spin the matcher (the bug that let one batch retry ~48k times).
	o.metrics.IncBatch(metrics.BatchTransientFail)
	o.nackBatch(batch)
	return o.backoff(shutdownCtx, transientBackoff)
}

// processBatchCaptured runs one batch through the repository while capturing the matching result
// and the funded count, so the caller can emit per-batch metrics. rejected is the number of open
// orders that failed reservation (incoming open orders minus funded).
func (o *OrderProcessor) processBatchCaptured(dbCtx context.Context, batch []*queuedEvent) (result *repository.BatchResult, rejected int, elapsed time.Duration, err error) {
	incoming := o.buildIncoming(batch)
	mf := o.buildMatch(batch)
	funded := 0
	start := time.Now()
	err = o.repo.ProcessBatch(dbCtx, incoming, func(fundedIDs []uuid.UUID) (*repository.BatchResult, error) {
		funded = len(fundedIDs)
		r, e := mf(fundedIDs)
		result = r
		return r, e
	})
	return result, len(incoming) - funded, time.Since(start), err
}

// recordCommitted emits the per-order outcome, trade, and book-depth metrics for a committed
// batch. result is nil only if ProcessBatch committed without invoking the match callback.
func (o *OrderProcessor) recordCommitted(result *repository.BatchResult, rejected int) {
	if result != nil {
		o.metrics.AddTrades(len(result.Matches))
		for i := range result.NewOrders {
			o.metrics.IncProcessed(result.NewOrders[i].Status)
		}
	}
	o.metrics.AddRejected(rejected)
	o.updateBookGauges()
}

// updateBookGauges snapshots the (post-commit) book depth into the gauges.
func (o *OrderProcessor) updateBookGauges() {
	s := o.book.Stats()
	o.metrics.SetBook(metrics.SideBuy, s.BidOrders, s.BestBid, s.HasBid)
	o.metrics.SetBook(metrics.SideSell, s.AskOrders, s.BestAsk, s.HasAsk)
}

// isolate reprocesses a data-error batch one order at a time so the healthy orders still
// commit, then pinpoints the poison order. An order that keeps failing deterministically
// is requeued until it exceeds maxOrderFailures, after which it is dead-lettered so it can
// never wedge the market again.
func (o *OrderProcessor) isolate(shutdownCtx, dbCtx context.Context, batch []*queuedEvent) bool {
	o.logger.Warn(fmt.Sprintf("order processor %s-%s: data error, isolating batch of %d to find the poison order",
		o.market.BaseSymbol, o.market.QuoteSymbol, len(batch)))

	requeued := false
	for i := range batch {
		qe := batch[i]
		single := batch[i : i+1]
		result, rejected, _, err := o.processBatchCaptured(dbCtx, single)
		if err == nil {
			// A healthy order committed on its own; record its outcome (but not batch_size /
			// batches_total — the drained batch was already counted as poison_isolated).
			o.recordCommitted(result, rejected)
			o.ackBatch(single)
			delete(o.failures, orderKey(qe))
			continue
		}

		// Single order failed; the book is dirty again.
		o.metrics.IncRebuild()
		if !o.loadBook(shutdownCtx, dbCtx) {
			o.nackBatch(batch[i:])
			return false
		}

		if !errors.Is(err, repository.ErrPoison) {
			// A transient blip mid-isolation — requeue this order and the remainder and
			// let the normal retry path handle it; do not blame the order.
			o.logger.Error(fmt.Sprintf("order processor %s-%s: transient error during isolation, requeueing remainder: %s",
				o.market.BaseSymbol, o.market.QuoteSymbol, err))
			o.nackBatch(batch[i:])
			return o.backoff(shutdownCtx, transientBackoff)
		}

		key := orderKey(qe)
		o.failures[key]++
		if o.failures[key] >= maxOrderFailures {
			o.logger.Error(fmt.Sprintf("order processor %s-%s: DEAD-LETTERING poison order %s after %d failures: %s",
				o.market.BaseSymbol, o.market.QuoteSymbol, key, o.failures[key], err))
			delete(o.failures, key)
			o.metrics.IncDeadLetter()
			if rerr := qe.delivery.Reject(); rerr != nil {
				o.logger.Error(fmt.Sprintf("order processor: reject (dead-letter) failed id=%s: %s", qe.delivery.ID(), rerr))
			}
			continue
		}
		o.logger.Warn(fmt.Sprintf("order processor %s-%s: poison candidate %s (failure %d/%d), requeueing: %s",
			o.market.BaseSymbol, o.market.QuoteSymbol, key, o.failures[key], maxOrderFailures, err))
		if nerr := qe.delivery.Nack(); nerr != nil {
			o.logger.Error(fmt.Sprintf("order processor: nack failed id=%s: %s", qe.delivery.ID(), nerr))
		}
		requeued = true
	}

	if requeued {
		// Pace re-attempts of requeued poison candidates.
		return o.backoff(shutdownCtx, poisonBackoff)
	}
	return true
}

// backoff sleeps for d unless shutdown is requested first; returns false on shutdown.
func (o *OrderProcessor) backoff(shutdownCtx context.Context, d time.Duration) bool {
	select {
	case <-shutdownCtx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func orderKey(qe *queuedEvent) uuid.UUID {
	if qe.open != nil {
		return qe.open.OrderID
	}
	if qe.cancel != nil {
		return qe.cancel.OrderID
	}
	return uuid.UUID{}
}

// loadBook rebuilds the in-memory book from the persisted open orders, retrying until
// it succeeds or shutdown is requested (returning false in that case). It is used both
// for initial hydration and for recovery after a failed batch.
func (o *OrderProcessor) loadBook(shutdownCtx, dbCtx context.Context) bool {
	for {
		rows, err := o.repo.LoadOpenOrders(dbCtx, o.market.ID)
		if err == nil {
			book := orderbook.NewOrderBook(o.logger, o.market)
			book.Hydrate(rows)
			o.book = book
			return true
		}
		o.logger.Error(fmt.Sprintf("order processor %s-%s: load book failed, retrying: %s",
			o.market.BaseSymbol, o.market.QuoteSymbol, err))
		select {
		case <-shutdownCtx.Done():
			return false
		case <-time.After(rebuildBackoff):
		}
	}
}

func (o *OrderProcessor) ackBatch(batch []*queuedEvent) {
	for _, qe := range batch {
		if err := qe.delivery.Ack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: ack failed id=%s: %s", qe.delivery.ID(), err))
		}
	}
}

func (o *OrderProcessor) nackBatch(batch []*queuedEvent) {
	for _, qe := range batch {
		if err := qe.delivery.Nack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: nack failed id=%s: %s", qe.delivery.ID(), err))
		}
	}
}

func reserveAmount(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

// coreMetrics may be nil, which disables per-market metric recording.
func NewOrderProcessor(
	log *logger.Logger,
	market *repository.Market,
	queue orderEventsQueue,
	repo orderRepository,
	coreMetrics *metrics.CoreMetrics,
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
		logger:  log,
		market:  market,
		queue:   queue,
		repo:    repo,
		metrics: coreMetrics.BindMarket(marketRef),
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
