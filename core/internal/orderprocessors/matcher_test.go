package orderprocessors

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/core/pkg/deadletter"
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
	r.mu.Unlock()
	return nil
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
	if _, err := match(fundedIDs(incoming, false)); err != nil {
		return err
	}
	return repository.ErrPoison
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

// An expiring order that can never be committed is quarantined instead of being re-derived every
// sweep. It has no broker message, so it cannot be dead-lettered and acked out of the way like a
// real order — without quarantine the market wedges permanently, rebuilding the book on every tick.
//
// The filter has to survive a rebuild: loadBook re-hydrates the expiry index from the DB, so
// removing the order from the book instead would resurrect it on the very next failure.
func TestMatcherQuarantinesPoisonExpiry(t *testing.T) {
	orderID := uuid.New()
	past := time.Now().Add(-time.Hour).Unix()
	repo := &poisonExpiryRepo{orders: []repository.OpenOrderHydration{restingHydration(orderID, &past)}}
	dlq := &fakeDeadLetterer{}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, dlq, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	// Each sweep costs the order one failure, so quarantine is maxOrderFailures ticks away.
	runUntilWithin(t, 2*maxOrderFailures*expirySweepInterval, func() bool { return dlq.count() == 1 })

	parked := dlq.parked[0]
	if parked.Reason != deadletter.ReasonQuarantined {
		t.Fatalf("parked reason=%q want %q", parked.Reason, deadletter.ReasonQuarantined)
	}
	if parked.OrderID != orderID.String() {
		t.Fatalf("parked order=%q want %q", parked.OrderID, orderID)
	}

	// Once quarantined the sweep must stop touching it, even though the book still holds it and
	// every rebuild re-indexes its TTL.
	settled, _ := repo.counts()
	time.Sleep(3 * expirySweepInterval)
	after, _ := repo.counts()
	if after != settled {
		t.Fatalf("quarantined order still swept: batches went %d -> %d", settled, after)
	}
}

// Quarantine must be forgettable. Once the order leaves the book — settled by an operator, cancelled
// by its owner, or filled — the set must drop it, or the gauge built on it (documented as "alert on
// > 0") would keep firing for a closed incident until the next core restart.
func TestPruneQuarantineForgetsOrdersThatLeftTheBook(t *testing.T) {
	gone, stillResting := uuid.New(), uuid.New()
	past := time.Now().Add(-time.Hour).Unix()
	repo := &expiryHydrationRepo{orders: []repository.OpenOrderHydration{restingHydration(stillResting, &past)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, nil, "")

	// Hydrate the book directly rather than via Start, so the matcher goroutine never races the
	// assertions — pruneQuarantine is normally only ever called from that goroutine.
	if !p.loadBook(context.Background(), context.Background()) {
		t.Fatal("hydration failed")
	}
	p.quarantined[gone] = struct{}{}
	p.quarantined[stillResting] = struct{}{}

	p.pruneQuarantine()

	if _, ok := p.quarantined[gone]; ok {
		t.Fatal("an order no longer in the book must be forgotten")
	}
	if _, ok := p.quarantined[stillResting]; !ok {
		t.Fatal("an order still resting is still quarantined — forgetting it would resume the sweep loop")
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
