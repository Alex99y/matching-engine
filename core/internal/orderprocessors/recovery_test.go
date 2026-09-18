package orderprocessors

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// A failed commit nacks the batch and rebuilds the book; on retry it commits and acks.
func TestMatcherRebuildsOnFailure(t *testing.T) {
	rec := &ackRecorder{}
	q := &fakeQueue{deliveries: []*oeq.OrderDelivery{rec.delivery(limitBuy())}}
	repo := &fakeRepo{failNext: 1}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), q, repo, nil, nil, &fakePoison{}, "")

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

// poisonBroker simulates a real broker: it redelivers nacked messages until each is
// acked, letting the matcher's isolation + dead-letter path run to completion.
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

func (b *poisonBroker) EmitCancelOrder(ctx context.Context, orderID uuid.UUID) error { return nil }
func (b *poisonBroker) Pause()                                                       {}
func (b *poisonBroker) Resume()                                                      {}
func (b *poisonBroker) IsPaused() bool                                               { return false }

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
			env := oeq.NewOpenOrderEvent(ev)
			raw, err := env.ToBytes()
			if err != nil {
				return err
			}
			handler(oeq.NewOrderDelivery(env, raw, id,
				func() error { b.mu.Lock(); b.acks[id]++; b.mu.Unlock(); return nil },
				func() error {
					b.mu.Lock()
					b.nacks[id]++
					b.mu.Unlock()
					select {
					case b.pending <- ev:
					default:
					}
					return nil
				},
				func() error { b.mu.Lock(); b.rejects[id]++; b.mu.Unlock(); return nil },
			))
		}
	}
}

// poisonRepo fails ProcessBatch with ErrPoison for any batch containing the poison order;
// healthy orders commit. It runs match (mutating the book) to mimic the real flush-time
// failure that leaves the book dirty.
type poisonRepo struct {
	noPending
	poison uuid.UUID
}

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

// A poison order is isolated: the healthy orders in its batch still commit, and the poison order is
// rejected after maxOrderFailures — the broker moves it to the dead-letter queue — unwedging the
// market. Its commit error is handed to the dead-letter consumer, which cannot recompute it.
func TestMatcherPoisonIsolation(t *testing.T) {
	good1, poison, good2 := limitBuy(), limitBuy(), limitBuy()
	b := newPoisonBroker(good1, poison, good2)
	repo := &poisonRepo{poison: poison.OrderID}
	recorder := &fakePoison{}
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(), b, repo, nil, nil, recorder, "")

	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)

	gid1, gid2, pid := good1.OrderID.String(), good2.OrderID.String(), poison.OrderID.String()
	runUntil(t, func() bool { return b.count(b.acks, gid1) == 1 && b.count(b.acks, gid2) == 1 })
	runUntil(t, func() bool { return b.count(b.rejects, pid) == 1 })
	cancel()

	if n := b.count(b.nacks, pid); n != maxOrderFailures-1 {
		t.Fatalf("poison nacks=%d want %d", n, maxOrderFailures-1)
	}
	// Rejected, never acked: the reject is what parks it, and an ack would drop it instead.
	if n := b.count(b.acks, pid); n != 0 {
		t.Fatalf("poison acks=%d want 0", n)
	}
	if text, ok := recorder.errorFor(pid, string(oeq.EventTypeOpenOrder)); !ok || !strings.Contains(text, "poison") {
		t.Fatalf("recorded error = (%q, %v), want the commit error handed to the consumer before the reject", text, ok)
	}
}
