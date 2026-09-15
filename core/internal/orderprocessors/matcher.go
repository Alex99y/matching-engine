package orderprocessors

import (
	"context"
	"time"
)

// This file is the matcher's own select loop: draining micro-batches from ordersChannel,
// firing the snapshot/heartbeat/sweep tickers (stream.go, expiry and trigger sweeps below), and
// assembling one batch's worth of events. batch.go turns a batch into a transaction.

// matcher is the single writer for this market. It drains micro-batches and processes each in one
// transaction, acking after commit or rebuilding the book on failure. The snapshot, heartbeat, and
// sweep tickers share this loop (rather than separate goroutines) so they can read/mutate the book
// it solely owns without a lock — see docs/event-log.md §7. The loop exits when the consumer
// closes ordersChannel on shutdown.
//
// The last trade price only moves inside a batch, so this loop is also the bracket watcher: after
// every committed batch it fires the exits that batch's trades woke (fireTriggered), and the sweep
// ticker is the backstop for anything a failed sweep left behind.
func (o *OrderProcessor) matcher(shutdownCtx, dbCtx context.Context) {
	snapshotTicker := time.NewTicker(snapshotInterval)
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	sweepTicker := time.NewTicker(expirySweepInterval)
	defer snapshotTicker.Stop()
	defer heartbeatTicker.Stop()
	defer sweepTicker.Stop()

	for {
		select {
		case first, ok := <-o.ordersChannel:
			if !ok {
				return // channel closed and drained
			}
			if o.runBatch(shutdownCtx, dbCtx, o.collectBatch(first)) == batchShutdown {
				return
			}
			if !o.fireTriggered(shutdownCtx, dbCtx) {
				return
			}
		case <-snapshotTicker.C:
			o.emitSnapshot()
		case <-heartbeatTicker.C:
			o.emitHeartbeat()
		case now := <-sweepTicker.C:
			if !o.book.HasDue(now.Unix()) && !o.book.HasTriggered() {
				continue
			}
			if o.runBatch(shutdownCtx, dbCtx, nil) == batchShutdown {
				return
			}
			if !o.fireTriggered(shutdownCtx, dbCtx) {
				return
			}
		}
	}
}

// fireTriggered drains the bracket exits whose trigger the last batch reached, one sweep-only
// batch at a time — each fire is a trade that can move the price and wake the next. It stops at
// the first batch that does not commit and leaves the rest to the sweep ticker, so a poisoned
// sweep cannot spin the matcher. Returns false only when shutdown was requested.
func (o *OrderProcessor) fireTriggered(shutdownCtx, dbCtx context.Context) bool {
	for o.book.HasTriggered() {
		switch o.runBatch(shutdownCtx, dbCtx, nil) {
		case batchShutdown:
			return false
		case batchRecovered:
			return true
		}
	}
	return true
}

// collectBatch extends the just-received first event into a micro-batch, collecting more without
// blocking until the batch is full or maxBatchWait elapses. A closed channel mid-collect just ends
// the batch early; the next matcher loop detects the closure and exits.
func (o *OrderProcessor) collectBatch(first *queuedEvent) []*queuedEvent {
	batch := make([]*queuedEvent, 0, maxBatchSize)
	batch = append(batch, first)

	timer := time.NewTimer(maxBatchWait)
	defer timer.Stop()
	for len(batch) < maxBatchSize {
		select {
		case qe, ok := <-o.ordersChannel:
			if !ok {
				return batch // channel closed mid-collect; process what we have
			}
			batch = append(batch, qe)
		case <-timer.C:
			return batch
		}
	}
	return batch
}
