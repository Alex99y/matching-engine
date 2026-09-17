package orderprocessors

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
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
	paused     atomic.Bool
	mu         sync.Mutex
	cancelled  []uuid.UUID
}

func (q *fakeQueue) WatchForOrderEvents(ctx context.Context, handler oeq.OrderDeliveryHandler) error {
	for _, d := range q.deliveries {
		handler(d)
	}
	<-ctx.Done()
	return nil
}

func (q *fakeQueue) EmitCancelOrder(ctx context.Context, orderID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancelled = append(q.cancelled, orderID)
	return nil
}

func (q *fakeQueue) cancelledOrders() []uuid.UUID {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]uuid.UUID(nil), q.cancelled...)
}

func (q *fakeQueue) Pause()         { q.paused.Store(true) }
func (q *fakeQueue) Resume()        { q.paused.Store(false) }
func (q *fakeQueue) IsPaused() bool { return q.paused.Load() }

// fakeRepo records calls and lets a test force ProcessBatch to fail a number of times.
// noPending satisfies the bracket-exit half of orderRepository for fakes whose tests never park an
// exit: nothing pending, market never traded.
type noPending struct{}

func (noPending) LoadPendingOrders(ctx context.Context, marketID int) ([]repository.PendingOrderHydration, error) {
	return nil, nil
}

func (noPending) LoadLastPrice(ctx context.Context, marketID int) (uint64, bool, error) {
	return 0, false, nil
}

type fakeRepo struct {
	noPending
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
	raw, err := env.ToBytes()
	if err != nil {
		panic(err)
	}
	return oeq.NewOrderDelivery(env, raw, open.OrderID.String(),
		func() error { a.mu.Lock(); a.acks++; a.mu.Unlock(); return nil },
		func() error { a.mu.Lock(); a.nacks++; a.mu.Unlock(); return nil },
		func() error { a.mu.Lock(); a.rejects++; a.mu.Unlock(); return nil },
	)
}

func (a *ackRecorder) rejected() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rejects
}

// fakePoison records the error texts the processor hands to the dead-letter consumer.
type fakePoison struct {
	mu       sync.Mutex
	recorded map[string]string // messageID|eventType → error text
}

func (f *fakePoison) Record(messageID, eventType, errText string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recorded == nil {
		f.recorded = map[string]string{}
	}
	f.recorded[messageID+"|"+eventType] = errText
}

func (f *fakePoison) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recorded)
}

func (f *fakePoison) errorFor(messageID, eventType string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	text, ok := f.recorded[messageID+"|"+eventType]
	return text, ok
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

// deliveryFor builds a delivery around an arbitrary envelope, recording ack/nack/reject. A nil
// event stands for a message the consumer could not parse at all.
func deliveryFor(rec *ackRecorder, event *oeq.OrderEvent, raw []byte) *oeq.OrderDelivery {
	return oeq.NewOrderDelivery(event, raw, "test-id",
		func() error { rec.mu.Lock(); rec.acks++; rec.mu.Unlock(); return nil },
		func() error { rec.mu.Lock(); rec.nacks++; rec.mu.Unlock(); return nil },
		func() error { rec.mu.Lock(); rec.rejects++; rec.mu.Unlock(); return nil },
	)
}

func newTestProcessor() *OrderProcessor {
	return NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(),
		&fakeQueue{}, &fakeRepo{}, nil, nil, &fakePoison{}, "")
}

// Every command that can never be processed is rejected without requeue — the broker moves it to
// the dead-letter queue in that same operation — and neither acked (that would drop it) nor
// nacked (that would redeliver a message guaranteed to fail again).
func TestHandleDeliveryRejectsUnprocessableCommands(t *testing.T) {
	unknownType, err := json.Marshal(oeq.OrderEvent{Type: "wat", Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	invalid := limitBuy()
	invalid.Quantity = 0 // fails ValidateOrderEvent
	invalidEnv, err := oeq.NewOpenOrderEvent(invalid)
	if err != nil {
		t.Fatal(err)
	}
	invalidRaw, err := invalidEnv.ToBytes()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		event *oeq.OrderEvent
		raw   []byte
	}{
		{name: "envelope did not parse", raw: []byte("not json at all")},
		{
			name:  "payload did not decode",
			event: &oeq.OrderEvent{Type: oeq.EventTypeOpenOrder, Payload: []byte(`{"price":"nope"}`)},
			raw:   []byte(`{"type":"open_order","payload":{"price":"nope"}}`),
		},
		{name: "unknown event type", event: &oeq.OrderEvent{Type: "wat", Payload: []byte(`{}`)}, raw: unknownType},
		{name: "invalid order", event: invalidEnv, raw: invalidRaw},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &ackRecorder{}
			p := newTestProcessor()

			p.handleDelivery(deliveryFor(rec, tt.event, tt.raw))

			a, n := rec.counts()
			if r := rec.rejected(); r != 1 || a != 0 || n != 0 {
				t.Fatalf("rejects=%d acks=%d nacks=%d want 1/0/0", r, a, n)
			}
			if len(p.ordersChannel) != 0 {
				t.Fatal("an unprocessable command must never reach the matcher")
			}
		})
	}
}

// A valid command reaches the matcher and is left unacknowledged — ack-after-commit owns it now.
func TestHandleDeliveryForwardsValidOrder(t *testing.T) {
	rec := &ackRecorder{}
	p := newTestProcessor()

	p.handleDelivery(rec.delivery(limitBuy()))

	a, n := rec.counts()
	if r := rec.rejected(); a != 0 || n != 0 || r != 0 {
		t.Fatalf("acks=%d nacks=%d rejects=%d want 0/0/0 — the matcher acks after commit", a, n, r)
	}
	if len(p.ordersChannel) != 1 {
		t.Fatalf("matcher received %d events, want 1", len(p.ordersChannel))
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

// The admin API halts a market through the processor, which owns nothing itself — it delegates to
// the queue, where stopping consumption actually happens.
func TestPauseAndResumeDelegateToTheQueue(t *testing.T) {
	q := &fakeQueue{}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, &fakeRepo{}, nil, nil, &fakePoison{}, "")

	if p.IsPaused() {
		t.Fatal("a new processor must start trading, not paused")
	}

	p.Pause()
	if !q.IsPaused() || !p.IsPaused() {
		t.Fatal("Pause did not reach the queue")
	}

	// Idempotent: the admin API is reachable at any time and a repeated pause must be harmless.
	p.Pause()
	if !p.IsPaused() {
		t.Fatal("a second Pause un-paused the market")
	}

	p.Resume()
	if q.IsPaused() || p.IsPaused() {
		t.Fatal("Resume did not reach the queue")
	}
}

// The admin API cancels a user's orders by publishing onto the market's own command queue, so the
// cancel is applied by the matcher in turn rather than by reaching into the book from another
// goroutine.
func TestEmitCancelPublishesToTheQueue(t *testing.T) {
	q := &fakeQueue{}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, &fakeRepo{}, nil, nil, &fakePoison{}, "")

	orderID := uuid.New()
	if err := p.EmitCancel(context.Background(), orderID); err != nil {
		t.Fatal(err)
	}

	got := q.cancelledOrders()
	if len(got) != 1 || got[0] != orderID {
		t.Fatalf("queue saw %v, want [%s]", got, orderID)
	}
}
