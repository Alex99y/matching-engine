package orderprocessors

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// fakeQueue replays a fixed set of deliveries to the handler, then blocks until ctx is
// cancelled so Start closes the channel and the matcher drains and exits.
type fakeQueue struct {
	deliveries []*oeq.OrderDelivery
}

func (q *fakeQueue) WatchForOrderEvents(ctx context.Context, handler oeq.OrderDeliveryHandler) error {
	for _, d := range q.deliveries {
		handler(d)
	}
	<-ctx.Done()
	return nil
}

// fakeRepo records calls and lets a test force ProcessBatch to fail a number of times.
type fakeRepo struct {
	mu            sync.Mutex
	processCalls  int
	loadCalls     int
	failNext      int32 // ProcessBatch returns an error this many more times
	fundNone      bool  // simulate every reservation failing (insufficient funds)
	matchedOrders []uuid.UUID
}

func (r *fakeRepo) ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error {
	r.mu.Lock()
	r.processCalls++
	r.mu.Unlock()

	if atomic.LoadInt32(&r.failNext) > 0 {
		atomic.AddInt32(&r.failNext, -1)
		// Mimic ProcessBatch's real contract: matching runs (mutating the book) before
		// the failure surfaces, so the processor must rebuild afterwards.
		funded := fundedIDs(incoming, r.fundNone)
		_, _ = match(funded)
		return context.DeadlineExceeded
	}

	funded := fundedIDs(incoming, r.fundNone)
	if _, err := match(funded); err != nil {
		return err
	}
	r.mu.Lock()
	r.matchedOrders = append(r.matchedOrders, funded...)
	r.mu.Unlock()
	return nil
}

func (r *fakeRepo) LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error) {
	r.mu.Lock()
	r.loadCalls++
	r.mu.Unlock()
	return nil, nil
}

func fundedIDs(incoming []repository.IncomingOrder, fundNone bool) []uuid.UUID {
	if fundNone {
		return nil
	}
	ids := make([]uuid.UUID, len(incoming))
	for i := range incoming {
		ids[i] = incoming[i].Insert.ID
	}
	return ids
}

type ackRecorder struct {
	mu      sync.Mutex
	acks    int
	nacks   int
	rejects int
}

func (a *ackRecorder) delivery(open *oeq.OpenOrderEvent) *oeq.OrderDelivery {
	env, err := oeq.NewOpenOrderEvent(open)
	if err != nil {
		panic(err)
	}
	return oeq.NewOrderDelivery(env, open.OrderID.String(),
		func() error { a.mu.Lock(); a.acks++; a.mu.Unlock(); return nil },
		func() error { a.mu.Lock(); a.nacks++; a.mu.Unlock(); return nil },
		func() error { a.mu.Lock(); a.rejects++; a.mu.Unlock(); return nil },
	)
}

func (a *ackRecorder) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acks, a.nacks
}

func testMarket() *repository.Market {
	return &repository.Market{ID: 1, BaseSymbol: "BTC", QuoteSymbol: "USDT", BaseInstrumentID: 10, QuoteInstrumentID: 20}
}

func limitBuy() *oeq.OpenOrderEvent {
	return &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 5,
	}
}

func runUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// A committed batch acks its deliveries and does not rebuild the book.
func TestMatcherAcksAfterCommit(t *testing.T) {
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy()), rec.delivery(limitBuy())}}
	repo := &fakeRepo{}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo)

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

// A failed commit nacks the batch and rebuilds the book; on retry it commits and acks.
func TestMatcherRebuildsOnFailure(t *testing.T) {
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy())}}
	repo := &fakeRepo{failNext: 1}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo)

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	// First attempt fails -> 1 nack + a rebuild; broker would requeue, but the fake
	// queue does not redeliver, so we just assert the failure handling fired.
	runUntil(t, func() bool { _, n := rec.counts(); return n == 1 })
	runUntil(t, func() bool { repo.mu.Lock(); defer repo.mu.Unlock(); return repo.loadCalls == 2 })
	cancel()

	_, n := rec.counts()
	if n != 1 {
		t.Fatalf("nacks=%d (want 1)", n)
	}
}

// Insufficient funds is a committed outcome: the delivery is acked and the book is
// NOT rebuilt.
func TestMatcherRejectionNoRebuild(t *testing.T) {
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy())}}
	repo := &fakeRepo{fundNone: true}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo)

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

// poisonBroker simulates a real broker: it redelivers nacked messages until each is
// acked or rejected, letting the matcher's isolation + dead-letter path run to completion.
type poisonBroker struct {
	pending chan *oeq.OpenOrderEvent
	mu      sync.Mutex
	acks    map[string]int
	nacks   map[string]int
	rejects map[string]int
}

func newPoisonBroker(events ...*oeq.OpenOrderEvent) *poisonBroker {
	b := &poisonBroker{
		pending: make(chan *oeq.OpenOrderEvent, 256),
		acks:    map[string]int{}, nacks: map[string]int{}, rejects: map[string]int{},
	}
	for _, e := range events {
		b.pending <- e
	}
	return b
}

func (b *poisonBroker) count(m map[string]int, id string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return m[id]
}

func (b *poisonBroker) WatchForOrderEvents(ctx context.Context, handler oeq.OrderDeliveryHandler) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-b.pending:
			id := ev.OrderID.String()
			env, err := oeq.NewOpenOrderEvent(ev)
			if err != nil {
				return err
			}
			handler(oeq.NewOrderDelivery(env, id,
				func() error { b.mu.Lock(); b.acks[id]++; b.mu.Unlock(); return nil },
				func() error { b.mu.Lock(); b.nacks[id]++; b.mu.Unlock(); select { case b.pending <- ev: default: }; return nil },
				func() error { b.mu.Lock(); b.rejects[id]++; b.mu.Unlock(); return nil },
			))
		}
	}
}

// poisonRepo fails ProcessBatch with ErrPoison for any batch containing the poison order;
// healthy orders commit. It runs match (mutating the book) to mimic the real flush-time
// failure that leaves the book dirty.
type poisonRepo struct{ poison uuid.UUID }

func (r *poisonRepo) ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error {
	ids := make([]uuid.UUID, len(incoming))
	hasPoison := false
	for i := range incoming {
		ids[i] = incoming[i].Insert.ID
		if ids[i] == r.poison {
			hasPoison = true
		}
	}
	if _, err := match(ids); err != nil {
		return err
	}
	if hasPoison {
		return repository.ErrPoison
	}
	return nil
}

func (r *poisonRepo) LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error) {
	return nil, nil
}

// A poison order is isolated: the healthy orders in its batch still commit, and the poison
// order is dead-lettered (rejected) after maxOrderFailures, unwedging the market.
func TestMatcherPoisonIsolation(t *testing.T) {
	good1, poison, good2 := limitBuy(), limitBuy(), limitBuy()
	b := newPoisonBroker(good1, poison, good2)
	repo := &poisonRepo{poison: poison.OrderID}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), b, repo)

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	gid1, gid2, pid := good1.OrderID.String(), good2.OrderID.String(), poison.OrderID.String()
	runUntil(t, func() bool { return b.count(b.acks, gid1) == 1 && b.count(b.acks, gid2) == 1 })
	runUntil(t, func() bool { return b.count(b.rejects, pid) == 1 })
	cancel()

	if n := b.count(b.nacks, pid); n != maxOrderFailures-1 {
		t.Fatalf("poison nacks=%d want %d", n, maxOrderFailures-1)
	}
	if b.count(b.acks, pid) != 0 {
		t.Fatalf("poison must never be acked")
	}
}
