package orderprocessors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/core/internal/metrics"
	"github.com/alex99y/matching-engine/core/internal/orderbook"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file turns one micro-batch into a committed transaction: building the reservation +
// matching inputs, running ProcessBatch, and recording the outcome. recovery.go handles what
// happens when this fails.

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
				} else {
					o.logger.Debug(fmt.Sprintf("Rejected order %s unsufficient balance", qe.open.OrderID))
					// Unfunded: rejected at reservation, never reaches the book. Notify its owner
					// so the private stream still carries a lifecycle event for it.
					o.book.RecordRejection(qe.open.UserID, qe.open.OrderID)
				}
			case qe.cancel != nil:
				o.book.CancelOrder(qe.cancel, result)
			case qe.expire != nil:
				o.book.ExpireOrder(*qe.expire, result)
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
		o.afterCommit(result, rejected)
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

// afterCommit runs the post-commit side effects for a successfully persisted batch: it records the
// metrics and publishes the live event-log deltas the batch produced (see stream.go). Both read the
// matcher's own committed data, never block, and run on the matcher goroutine.
func (o *OrderProcessor) afterCommit(result *repository.BatchResult, rejected int) {
	o.recordCommitted(result, rejected)
	o.publishStream()
}

// ackBatch and nackBatch skip a nil delivery: a synthetic expiry event (see buildExpiryBatch)
// has no broker message behind it, so there is nothing to ack/nack.
func (o *OrderProcessor) ackBatch(batch []*queuedEvent) {
	for _, qe := range batch {
		if qe.delivery == nil {
			continue
		}
		if err := qe.delivery.Ack(); err != nil {
			o.logger.Error(fmt.Sprintf("order processor: ack failed id=%s: %s", qe.delivery.ID(), err))
		}
	}
}

func (o *OrderProcessor) nackBatch(batch []*queuedEvent) {
	for _, qe := range batch {
		if qe.delivery == nil {
			continue
		}
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
