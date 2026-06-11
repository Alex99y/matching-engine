package orderprocessors

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/core/internal/orderbook"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

const (
	orderChannelBuffer = 64
	maxBatchSize       = 32
	maxBatchWait       = 5 * time.Millisecond
	rebuildBackoff     = 2 * time.Second
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
	stopMatcher   atomic.Bool
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

// runBatch processes one micro-batch in a single transaction. It returns false only
// when shutdown is requested while recovering from a failure.
func (o *OrderProcessor) runBatch(shutdownCtx, dbCtx context.Context, batch []*queuedEvent) bool {
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

	// match runs in-memory under ProcessBatch's transaction, after funds are reserved.
	// It replays the batch in arrival order so cancels and opens interleave with strict
	// FIFO priority; only funded opens reach the book.
	match := func(fundedOrderIDs []uuid.UUID) (*repository.BatchResult, error) {
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

	if err := o.repo.ProcessBatch(dbCtx, incoming, match); err != nil {
		o.logger.Error(fmt.Sprintf("order processor %s-%s: batch failed, rebuilding book: %s",
			o.market.BaseSymbol, o.market.QuoteSymbol, err))
		// The match callback already mutated the book before the rollback, so it is
		// dirty: requeue the batch and rebuild from the last committed DB state.
		o.nackBatch(batch)
		return o.loadBook(shutdownCtx, dbCtx)
	}

	o.ackBatch(batch)
	return true
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

func NewOrderProcessor(
	log *logger.Logger,
	market *repository.Market,
	queue orderEventsQueue,
	repo orderRepository,
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

	return &OrderProcessor{
		logger: log,
		market: market,
		queue:  queue,
		repo:   repo,
		constraints: oeq.MarketConstraints{
			PriceQuantum:  market.PriceQuantum,
			AmountQuantum: market.AmountQuantum,
			MinOrderSize:  market.MinOrderSize,
			MaxOrderSize:  market.MaxOrderSize,
		},
		ordersChannel: make(chan *queuedEvent, orderChannelBuffer),
	}
}
