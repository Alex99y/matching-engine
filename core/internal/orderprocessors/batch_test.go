package orderprocessors

import (
	"context"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
)

// A committed batch acks its deliveries and does not rebuild the book.
func TestMatcherAcksAfterCommit(t *testing.T) {
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy()), rec.delivery(limitBuy())}}
	repo := &fakeRepo{}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	runUntil(t, func() bool { a, _ := rec.counts(); return a == 2 })
	cancel()

	a, n := rec.counts()
	if a != 2 || n != 0 {
		t.Fatalf("acks=%d nacks=%d (want 2, 0)", a, n)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.loadCalls != 1 { // only the initial hydration
		t.Fatalf("loadCalls=%d (want 1, no rebuild on success)", repo.loadCalls)
	}
}

// Insufficient funds is a committed outcome: the delivery is acked and the book is
// NOT rebuilt.
func TestMatcherRejectionNoRebuild(t *testing.T) {
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy())}}
	repo := &fakeRepo{fundNone: true}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	runUntil(t, func() bool { a, _ := rec.counts(); return a == 1 })
	cancel()

	a, n := rec.counts()
	if a != 1 || n != 0 {
		t.Fatalf("acks=%d nacks=%d (want 1, 0)", a, n)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.loadCalls != 1 {
		t.Fatalf("loadCalls=%d (want 1, rejection must not rebuild)", repo.loadCalls)
	}
}
