package orderprocessors

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
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
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	runUntil(t, func() bool {
		_, closed := repo.snapshot()
		return len(closed) == 1 && closed[0] == orderID
	})
	cancel()
}

// A resting order that is not yet due must survive sweep after sweep untouched — the ticker
// must not fall back to a full book scan that would catch it regardless of its expiry.
func TestMatcherDoesNotExpireOrderBeforeItsTTL(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	repo := &expiryHydrationRepo{orders: []repository.OpenOrderHydration{restingHydration(uuid.New(), &future)}}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), &fakeQueue{}, repo, nil, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	time.Sleep(expirySweepInterval + 300*time.Millisecond) // let at least one sweep tick fire
	cancel()

	if batches, _ := repo.snapshot(); batches != 0 {
		t.Fatalf("ProcessBatch called %d time(s) for an order not yet due", batches)
	}
}
