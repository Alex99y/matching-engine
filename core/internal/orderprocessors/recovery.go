package orderprocessors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/core/internal/orderbook"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file handles what happens when a batch fails: rebuilding the book from the DB,
// isolating a poison order so the healthy ones still commit, and pacing retries.

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
		result, rejected, _, err := o.processBatchCaptured(dbCtx, single, false)
		if err == nil {
			// A healthy order committed on its own; record its outcome (but not batch_size /
			// batches_total — the drained batch was already counted as poison_isolated).
			o.afterCommit(result, rejected)
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
			failures := o.failures[key]
			delete(o.failures, key)
			o.parkPoison(dbCtx, qe, key, failures, err)
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
