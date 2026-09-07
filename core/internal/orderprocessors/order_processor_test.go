package orderprocessors

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/core/pkg/deadletter"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// This file holds the fixtures shared by every _test.go in this package: a fake broker
// queue, a fake repository, an ack/nack recorder, a fake dead-letter publisher, and common
// test builders. No Test functions live here.

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
	mu    sync.Mutex
	acks  int
	nacks int
}

func (a *ackRecorder) delivery(open *oeq.OpenOrderEvent) *oeq.OrderDelivery {
	env, err := oeq.NewOpenOrderEvent(open)
	if err != nil {
		panic(err)
	}
	raw, err := env.ToBytes()
	if err != nil {
		panic(err)
	}
	return oeq.NewOrderDelivery(env, raw, open.OrderID.String(),
		func() error { a.mu.Lock(); a.acks++; a.mu.Unlock(); return nil },
		func() error { a.mu.Lock(); a.nacks++; a.mu.Unlock(); return nil },
	)
}

// fakeDeadLetterer records what the processor parks. failWith makes Publish fail, exercising the
// ack-and-drop path.
type fakeDeadLetterer struct {
	mu       sync.Mutex
	parked   []deadletter.Envelope
	failWith error
}

func (f *fakeDeadLetterer) Publish(ctx context.Context, env *deadletter.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.parked = append(f.parked, *env)
	return nil
}

func (f *fakeDeadLetterer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.parked)
}

func (f *fakeDeadLetterer) reasons() []deadletter.Reason {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]deadletter.Reason, 0, len(f.parked))
	for _, e := range f.parked {
		out = append(out, e.Reason)
	}
	return out
}

func (a *ackRecorder) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acks, a.nacks
}

func testMarket() *repository.Market {
	// BaseScale: 1 == decimals=0, so quoteAmount's price*qty/BaseScale stays unscaled —
	// matches production, which always populates this from GetMarket (see
	// db/pkg/repository/markets.go); a zero-value Market here divides by zero.
	return &repository.Market{ID: 1, BaseSymbol: "BTC", QuoteSymbol: "USDT", BaseInstrumentID: 10, QuoteInstrumentID: 20, BaseScale: 1}
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
	runUntilWithin(t, 5*time.Second, cond)
}

func runUntilWithin(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
