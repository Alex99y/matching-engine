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

// bracketRepo hydrates a book with resting orders, parked exits and a last price, and records the
// status updates of every batch it commits — the only way a test can see an exit fire without
// touching the matcher-owned book. poisonExit, when set, makes any batch that writes that exit's
// status fail deterministically.
type bracketRepo struct {
	mu         sync.Mutex
	open       []repository.OpenOrderHydration
	pending    []repository.PendingOrderHydration
	lastPrice  uint64
	traded     bool
	poisonExit uuid.UUID

	batches  int
	hydrates int
	updates  []repository.OrderStatusUpdate
}

func (r *bracketRepo) ProcessBatch(ctx context.Context, incoming []repository.IncomingOrder, match repository.MatchFunc) error {
	r.mu.Lock()
	r.batches++
	r.mu.Unlock()
	result, err := match(fundedIDs(incoming, false))
	if err != nil {
		return err
	}
	for _, u := range result.StatusUpdates {
		if u.OrderID == r.poisonExit {
			return repository.ErrPoison
		}
	}
	r.mu.Lock()
	r.updates = append(r.updates, result.StatusUpdates...)
	r.mu.Unlock()
	return nil
}

func (r *bracketRepo) LoadOpenOrders(ctx context.Context, marketID int) ([]repository.OpenOrderHydration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hydrates++
	return r.open, nil
}

func (r *bracketRepo) LoadPendingOrders(ctx context.Context, marketID int) ([]repository.PendingOrderHydration, error) {
	return r.pending, nil
}

func (r *bracketRepo) LoadLastPrice(ctx context.Context, marketID int) (uint64, bool, error) {
	return r.lastPrice, r.traded, nil
}

func (r *bracketRepo) statusOf(id uuid.UUID) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.updates {
		if u.OrderID == id {
			return u.Status, true
		}
	}
	return "", false
}

func (r *bracketRepo) counts() (batches, hydrates int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batches, r.hydrates
}

func resting(side string, price, base uint64) repository.OpenOrderHydration {
	have, want := base, price*base
	if side == "buy" {
		have, want = price*base, base
	}
	return repository.OpenOrderHydration{
		OrderID: uuid.New(), UserID: uuid.New(), Side: side, Price: price, Type: "limit",
		TimeInForce: "GTC", RemainingHaveAmount: have, RemainingWantAmount: want,
	}
}

func parkedSellExit(tp uint64, base uint64) repository.PendingOrderHydration {
	entry := uuid.New()
	return repository.PendingOrderHydration{
		OrderID: oeq.ExitOrderID(entry), UserID: uuid.New(), ParentOrderID: entry,
		Side: "sell", Amount: base, TakeProfitPrice: &tp,
	}
}

// The watcher is the matcher loop itself: the batch that prints the trigger price is followed by a
// sweep-only batch that fires the exit, well inside the 1 s ticker that would otherwise be the
// only thing looking. The exit and the price both come from hydration, so this also proves
// LoadPendingOrders / LoadLastPrice are wired into the rebuilt book.
func TestMatcherFiresHydratedExitAsSoonAsThePriceIsReached(t *testing.T) {
	exit := parkedSellExit(150, 10)
	repo := &bracketRepo{
		open:      []repository.OpenOrderHydration{resting("sell", 150, 1), resting("buy", 140, 10)},
		pending:   []repository.PendingOrderHydration{exit},
		lastPrice: 100, traded: true,
	}
	taker := limitBuy()
	taker.Price, taker.Quantity, taker.TimeInForce = 150, 1, oeq.ImmediateOrCancel
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(taker)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	runUntilWithin(t, expirySweepInterval/2, func() bool {
		status, ok := repo.statusOf(exit.OrderID)
		return ok && status == repository.OrderStatusFilled
	})
	if batches, _ := repo.counts(); batches != 2 {
		t.Fatalf("ProcessBatch called %d time(s), want 2: the taker's batch and one sweep firing the exit", batches)
	}
}

// One tick short of the trigger nothing fires — not on the taker's batch and not on the sweep
// ticker either, which must not run a batch for an exit that is not due.
func TestMatcherLeavesExitParkedBelowItsTrigger(t *testing.T) {
	exit := parkedSellExit(150, 10)
	repo := &bracketRepo{
		open:      []repository.OpenOrderHydration{resting("sell", 149, 1), resting("buy", 140, 10)},
		pending:   []repository.PendingOrderHydration{exit},
		lastPrice: 100, traded: true,
	}
	taker := limitBuy()
	taker.Price, taker.Quantity, taker.TimeInForce = 149, 1, oeq.ImmediateOrCancel
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(taker)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	runUntil(t, func() bool { a, _ := rec.counts(); return a == 1 })
	time.Sleep(expirySweepInterval + 300*time.Millisecond)
	cancel()

	if _, ok := repo.statusOf(exit.OrderID); ok {
		t.Fatal("exit fired below its trigger")
	}
	if batches, _ := repo.counts(); batches != 1 {
		t.Fatalf("ProcessBatch called %d time(s), want 1 — no sweep for an exit that is not due", batches)
	}
}

// An exit whose settlement write is poisoned stays due after every rebuild. The drain after a
// batch must stop at the first failed sweep and leave the retry to the ticker, or the matcher
// would spin: rebuild, sweep, fail, rebuild, sweep, fail — with no batch ever committing.
func TestPoisonedTriggerSweepDoesNotSpinTheMatcher(t *testing.T) {
	exit := parkedSellExit(150, 10)
	repo := &bracketRepo{
		open:      []repository.OpenOrderHydration{resting("buy", 140, 10)},
		pending:   []repository.PendingOrderHydration{exit},
		lastPrice: 150, traded: true, // due from the first sweep
		poisonExit: exit.OrderID,
	}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	const ticks = 3
	time.Sleep(ticks*expirySweepInterval + expirySweepInterval/2)
	cancel()

	// Each tick may try the sweep and one drain attempt; anything well beyond that is a spin.
	batches, hydrates := repo.counts()
	if batches > 2*ticks+2 {
		t.Fatalf("ProcessBatch called %d time(s) in %d ticks — the poisoned sweep is spinning", batches, ticks)
	}
	if hydrates < 2 {
		t.Fatalf("hydrated %d time(s), want the book rebuilt after each failed sweep", hydrates)
	}
}
