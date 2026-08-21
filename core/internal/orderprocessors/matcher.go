package orderprocessors

import (
	"context"
	"time"
)

// This file is the matcher's own select loop: draining micro-batches from ordersChannel,
// firing the snapshot/heartbeat/expiry tickers (stream.go, expiry sweep below), and
// assembling one batch's worth of events. batch.go turns a batch into a transaction.

// matcher is the single writer for this market. It drains micro-batches and processes each in one
// transaction, acking after commit or rebuilding the book on failure. The snapshot, heartbeat, and
// expiry-sweep tickers share this loop (rather than separate goroutines) so they can read/mutate
// the book it solely owns without a lock — see docs/event-log.md §7. The loop exits when the
// consumer closes ordersChannel on shutdown.
func (o *OrderProcessor) matcher(shutdownCtx, dbCtx context.Context) {
	snapshotTicker := time.NewTicker(snapshotInterval)
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	expiryTicker := time.NewTicker(expirySweepInterval)
	defer snapshotTicker.Stop()
	defer heartbeatTicker.Stop()
	defer expiryTicker.Stop()

	for {
		select {
		case first, ok := <-o.ordersChannel:
			if !ok {
				return // channel closed and drained
			}
			if !o.runBatch(shutdownCtx, dbCtx, o.collectBatch(first)) {
				return // shutdown requested during recovery
			}
		case <-snapshotTicker.C:
			o.emitSnapshot()
		case <-heartbeatTicker.C:
			o.emitHeartbeat()
		case now := <-expiryTicker.C:
			if batch := o.buildExpiryBatch(now); batch != nil {
				if !o.runBatch(shutdownCtx, dbCtx, batch) {
					return // shutdown requested during recovery
				}
			}
		}
	}
}

// buildExpiryBatch asks the book (in-memory, no I/O) which resting orders are due, and wraps
// each as a synthetic cancel-like event with no broker delivery. nil means nothing was due —
// the common case for most ticks, since ExpireDue only walks the due prefix of its index.
func (o *OrderProcessor) buildExpiryBatch(now time.Time) []*queuedEvent {
	due := o.book.ExpireDue(now.Unix())
	if len(due) == 0 {
		return nil
	}
	batch := make([]*queuedEvent, 0, len(due))
	for i := range due {
		batch = append(batch, &queuedEvent{expire: &due[i]})
	}
	return batch
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
