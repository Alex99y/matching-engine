package orderprocessors

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// expiryHydrationRepo hydrates the book with a fixed set of resting orders and records
// ClosedOpenOrders from every batch it commits, so a test can observe which orders the
// expiry sweep actually reaped without touching OrderProcessor.book directly — that field is
// owned exclusively by the matcher goroutine, so the test must not read it.
type expiryHydrationRepo struct {
	mu      sync.Mutex
	orders  []repository.OpenOrderHydration
	batches int
	closed  []uuid.UUID
	matches int
}

func (r *expiryHydrationRepo) ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error {
	r.mu.Lock()
	r.batches++
	r.mu.Unlock()
	result, err := match(fundedIDs(incoming, false))
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.closed = append(r.closed, result.ClosedOpenOrders...)
	r.matches += len(result.Matches)
	r.mu.Unlock()
	return nil
}

func (r *expiryHydrationRepo) matchCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.matches
}

func (r *expiryHydrationRepo) LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.orders, nil
}

func (r *expiryHydrationRepo) snapshot() (batches int, closed []uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batches, append([]uuid.UUID(nil), r.closed...)
}

func restingHydration(id uuid.UUID, expiresAt *int64) repository.OpenOrderHydration {
	return repository.OpenOrderHydration{
		OrderID: id, UserID: uuid.New(), Side: "sell", Price: 100, Type: "limit",
		TimeInForce: "GTC", RemainingHaveAmount: 10, RemainingWantAmount: 1000,
		ExpiresAt: expiresAt,
	}
}

// The expiry sweep ticker reaps a resting order past its TTL through the normal batch
// pipeline — no live order/cancel traffic is involved, only the ticker in matcher's select
// loop — and the synthetic event's nil delivery must not upset ack handling.
func TestMatcherExpiresRestingOrderOnSweep(t *testing.T) {
	orderID := uuid.New()
	past := time.Now().Add(-time.Hour).Unix()
	repo := &expiryHydrationRepo{orders: []repository.OpenOrderHydration{restingHydration(orderID, &past)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	runUntil(t, func() bool {
		_, closed := repo.snapshot()
		return len(closed) == 1 && closed[0] == orderID
	})
	cancel()
}

// poisonExpiryRepo fails every batch with ErrPoison and re-hydrates the same resting order, so the
// expiry sweep keeps re-deriving it: the exact shape of the permanent isolate/rebuild loop that
// quarantine exists to break. It also counts hydrations, which is what makes the wedge observable.
type poisonExpiryRepo struct {
	mu       sync.Mutex
	orders   []repository.OpenOrderHydration
	batches  int
	hydrates int
}

func (r *poisonExpiryRepo) ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error {
	r.mu.Lock()
	r.batches++
	r.mu.Unlock()
	result, err := match(fundedIDs(incoming, false))
	if err != nil {
		return err
	}
	// The poison is the expiry's own settlement write, not the batch as a whole: only a result
	// carrying a closed resting order (which only the sweep produces here) fails. A batch whose
	// sweep was suppressed — every isolated one — must commit, or the test could not tell
	// "isolation skips the sweep" from "isolation is broken".
	if len(result.ClosedOpenOrders) > 0 {
		return repository.ErrPoison
	}
	return nil
}

func (r *poisonExpiryRepo) LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hydrates++
	return r.orders, nil
}

func (r *poisonExpiryRepo) counts() (batches, hydrates int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batches, r.hydrates
}

// An expiry whose settlement write fails deterministically must not take the market down with it,
// and — the sharp edge — must never get a healthy order dead-lettered in its place.
//
// isolate replays a failed batch one event at a time through the same match callback. If that
// callback re-ran the sweep, the poison expiry would fail every one of those single-order
// transactions, isolate would blame each innocent order, and after maxOrderFailures it would park a
// perfectly good order in the dead-letter queue. sweepExpiries=false during isolation is what
// prevents that, and this test is its guard.
func TestPoisonExpiryNeverDeadLettersAHealthyOrder(t *testing.T) {
	expiring := uuid.New()
	past := time.Now().Add(-time.Hour).Unix()
	repo := &poisonExpiryRepo{orders: []repository.OpenOrderHydration{restingHydration(expiring, &past)}}
	dlq := &fakeDeadLetterer{}
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy())}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, dlq, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	// Long enough for the sweep to have failed far more than maxOrderFailures times.
	time.Sleep(2 * maxOrderFailures * expirySweepInterval)

	if n := dlq.count(); n != 0 {
		t.Fatalf("parked %d command(s) — a poison expiry must never dead-letter an order: %v", n, dlq.reasons())
	}
	// The real order isolates cleanly with the sweep off, so it commits and is acked.
	if a, _ := rec.counts(); a != 1 {
		t.Fatalf("healthy order acks=%d, want 1 — isolation must still commit real orders", a)
	}
	if batches, _ := repo.counts(); batches == 0 {
		t.Fatal("expected the sweep to have been attempted")
	}
}

// The correctness win: expiry is swept at the start of the batch, before the batch's own events are
// replayed, so a taker can never trade against a maker whose TTL has already elapsed. Under the old
// 1 s ticker there was a window of up to a second in which exactly that could happen.
func TestIncomingTakerCannotTradeAgainstAnExpiredMaker(t *testing.T) {
	maker := uuid.New()
	past := time.Now().Add(-time.Hour).Unix()
	repo := &expiryHydrationRepo{orders: []repository.OpenOrderHydration{restingHydration(maker, &past)}}

	// restingHydration rests a sell at 100; this buy crosses it and would fill but for the sweep.
	taker := limitBuy()
	taker.Price = 100
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(taker)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	runUntil(t, func() bool { a, _ := rec.counts(); return a == 1 })

	_, closed := repo.snapshot()
	if len(closed) != 1 || closed[0] != maker {
		t.Fatalf("expired maker was not retired: closed=%v", closed)
	}
	if n := repo.matchCount(); n != 0 {
		t.Fatalf("taker traded against an expired maker (%d match(es))", n)
	}
}

// A resting order that is not yet due must survive sweep after sweep untouched — the ticker
// must not fall back to a full book scan that would catch it regardless of its expiry.
func TestMatcherDoesNotExpireOrderBeforeItsTTL(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	repo := &expiryHydrationRepo{orders: []repository.OpenOrderHydration{restingHydration(uuid.New(), &future)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	time.Sleep(expirySweepInterval + 300*time.Millisecond) // let at least one sweep tick fire
	cancel()

	if batches, _ := repo.snapshot(); batches != 0 {
		t.Fatalf("ProcessBatch called %d time(s) for an order not yet due", batches)
	}
}
