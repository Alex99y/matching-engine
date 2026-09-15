package orderprocessors

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/core/internal/orderbook"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

type published struct {
	routingKey string
	envelope   marketdata.Envelope
}

// recordingPublisher captures what reached the event-log exchange. full makes every Enqueue fail,
// standing in for a broker the publisher cannot keep up with.
type recordingPublisher struct {
	mu   sync.Mutex
	got  []published
	full bool
}

func (p *recordingPublisher) Enqueue(routingKey, messageId string, body []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.full {
		return false
	}
	var env marketdata.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		panic("publisher received a body that is not an envelope: " + err.Error())
	}
	p.got = append(p.got, published{routingKey: routingKey, envelope: env})
	return true
}

func (p *recordingPublisher) events() []published {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]published(nil), p.got...)
}

func (p *recordingPublisher) ofType(t marketdata.EventType) []published {
	var out []published
	for _, e := range p.events() {
		if e.envelope.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func newStreamProcessor(t *testing.T, pub eventPublisher) *OrderProcessor {
	t.Helper()
	p := NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(),
		&fakeQueue{}, &fakeRepo{}, nil, pub, &fakeDeadLetterer{}, "epoch-1")
	// Start would hydrate this from the DB; these tests drive the stream directly.
	p.book = orderbook.NewOrderBook(p.logger, p.market)
	return p
}

// restOrder puts a resting order in the book so the next drain has book deltas to publish.
func restOrder(t *testing.T, p *OrderProcessor, side oeq.OrderSide, price, qty uint64) {
	t.Helper()
	order := &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: side, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: price, Quantity: qty,
	}
	p.book.MatchOrder(order, &repository.BatchResult{})
}

// The API applies a book delta only when its seq is exactly one past the last it applied, so the
// deltas in a batch must come out numbered consecutively from wherever the stream had got to. A
// duplicated or skipped number makes every consumer discard the stream and re-sync.
func TestBookDeltasAdvanceSeqOneAtATime(t *testing.T) {
	pub := &recordingPublisher{}
	p := newStreamProcessor(t, pub)

	restOrder(t, p, oeq.BuyOrder, 100, 10)
	restOrder(t, p, oeq.BuyOrder, 99, 10)
	restOrder(t, p, oeq.SellOrder, 101, 10)
	p.publishStream()

	books := pub.ofType(marketdata.EventBook)
	if len(books) != 3 {
		t.Fatalf("published %d book deltas, want 3", len(books))
	}
	for i, e := range books {
		if want := uint64(i + 1); e.envelope.Seq != want {
			t.Fatalf("book delta %d has seq %d, want %d", i, e.envelope.Seq, want)
		}
	}
	if p.seq.Load() != 3 {
		t.Fatalf("seq = %d after 3 deltas, want 3", p.seq.Load())
	}

	// A second batch continues the same run rather than restarting it.
	restOrder(t, p, oeq.BuyOrder, 98, 10)
	p.publishStream()

	books = pub.ofType(marketdata.EventBook)
	if len(books) != 4 || books[3].envelope.Seq != 4 {
		t.Fatalf("second batch did not continue the sequence: %+v", books)
	}
}

// Only book deltas move the sequence. Everything else is stamped with the current value so a
// consumer can tell where it sits in the stream — if a trade or a snapshot advanced seq, consumers
// would see a gap in the deltas they actually apply and re-sync for no reason.
func TestOnlyBookDeltasAdvanceTheSequence(t *testing.T) {
	tests := []struct {
		name string
		emit func(*OrderProcessor)
	}{
		{"snapshot", func(p *OrderProcessor) { p.emitSnapshot() }},
		{"heartbeat", func(p *OrderProcessor) { p.emitHeartbeat() }},
		{"out-of-band order update", func(p *OrderProcessor) {
			p.publishOrderUpdate(uuid.New(), uuid.New(), "dead_lettered", 0, 0)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &recordingPublisher{}
			p := newStreamProcessor(t, pub)

			restOrder(t, p, oeq.BuyOrder, 100, 10)
			p.publishStream()
			before := p.seq.Load()

			tt.emit(p)

			if after := p.seq.Load(); after != before {
				t.Fatalf("seq moved from %d to %d", before, after)
			}
			last := pub.events()[len(pub.events())-1]
			if last.envelope.Seq != before {
				t.Fatalf("event stamped seq %d, want the current %d", last.envelope.Seq, before)
			}
		})
	}
}

// The book's sequence has to advance before a trade references it, or a consumer would see a fill at
// a price level whose change it has not been told about yet.
func TestBookDeltasArePublishedBeforeTrades(t *testing.T) {
	pub := &recordingPublisher{}
	p := newStreamProcessor(t, pub)

	restOrder(t, p, oeq.SellOrder, 100, 10)
	p.publishStream()

	taker := &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10,
	}
	p.book.MatchOrder(taker, &repository.BatchResult{})
	p.publishStream()

	var sawTrade bool
	for _, e := range pub.events() {
		switch e.envelope.Type {
		case marketdata.EventTrade:
			sawTrade = true
		case marketdata.EventBook:
			if sawTrade {
				t.Fatal("a book delta was published after a trade in the same batch")
			}
		}
	}
	if !sawTrade {
		t.Fatal("the crossing order produced no trade")
	}
}

// Public events are addressed to the market; a private order update is addressed to one user and
// must never go out on a public key, or one account's activity would reach every subscriber.
func TestRoutingKeysSeparatePublicFromPrivate(t *testing.T) {
	pub := &recordingPublisher{}
	p := newStreamProcessor(t, pub)

	userID := uuid.New()
	p.publishOrderUpdate(userID, uuid.New(), "dead_lettered", 0, 0)
	p.emitSnapshot()
	p.emitHeartbeat()

	events := pub.events()
	if got, want := events[0].routingKey, marketdata.PrivateKey(userID.String(), marketdata.EventOrder); got != want {
		t.Fatalf("order update routed to %q, want %q", got, want)
	}
	for _, e := range events[1:] {
		if want := marketdata.PublicKey(p.marketRef, e.envelope.Type); e.routingKey != want {
			t.Fatalf("%s routed to %q, want %q", e.envelope.Type, e.routingKey, want)
		}
	}
}

// Epoch is how a consumer detects that core restarted and its sequence numbers began again. Every
// envelope has to carry it, whichever path emitted it.
func TestEveryEnvelopeCarriesTheEpochAndMarket(t *testing.T) {
	pub := &recordingPublisher{}
	p := newStreamProcessor(t, pub)

	restOrder(t, p, oeq.BuyOrder, 100, 10)
	p.publishStream()
	p.emitSnapshot()
	p.emitHeartbeat()
	p.publishOrderUpdate(uuid.New(), uuid.New(), "dead_lettered", 0, 0)

	events := pub.events()
	if len(events) == 0 {
		t.Fatal("nothing was published")
	}
	for _, e := range events {
		if e.envelope.Epoch != "epoch-1" {
			t.Fatalf("%s envelope has epoch %q, want epoch-1", e.envelope.Type, e.envelope.Epoch)
		}
		if e.envelope.Market != p.marketRef {
			t.Fatalf("%s envelope has market %q, want %q", e.envelope.Type, e.envelope.Market, p.marketRef)
		}
		if e.envelope.Ts == 0 {
			t.Fatalf("%s envelope has no timestamp", e.envelope.Type)
		}
	}
}

// A snapshot is what a fresh or recovering API bootstraps from, so it has to carry the book as it
// stands at the seq it is stamped with.
func TestSnapshotCarriesTheBookAtTheCurrentSeq(t *testing.T) {
	pub := &recordingPublisher{}
	p := newStreamProcessor(t, pub)

	restOrder(t, p, oeq.BuyOrder, 100, 10)
	restOrder(t, p, oeq.SellOrder, 101, 7)
	p.publishStream()
	p.emitSnapshot()

	snapshots := pub.ofType(marketdata.EventSnapshot)
	if len(snapshots) != 1 {
		t.Fatalf("published %d snapshots, want 1", len(snapshots))
	}

	var snap marketdata.Snapshot
	if err := json.Unmarshal(snapshots[0].envelope.Payload, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Epoch != "epoch-1" || snap.Market != p.marketRef {
		t.Fatalf("snapshot identity = %q/%q", snap.Epoch, snap.Market)
	}
	if snap.Seq != p.seq.Load() || snapshots[0].envelope.Seq != snap.Seq {
		t.Fatalf("snapshot seq %d disagrees with the stream at %d", snap.Seq, p.seq.Load())
	}
	if len(snap.Bids) != 1 || len(snap.Asks) != 1 {
		t.Fatalf("snapshot = %d bids, %d asks; want 1 and 1", len(snap.Bids), len(snap.Asks))
	}
}

// The event log is best-effort by design: a publisher that cannot accept an event must not stall or
// crash the matcher. The gap it leaves is what tells the consumer to re-sync.
func TestAFullPublisherDoesNotStopTheMatcher(t *testing.T) {
	pub := &recordingPublisher{full: true}
	p := newStreamProcessor(t, pub)

	restOrder(t, p, oeq.BuyOrder, 100, 10)
	p.publishStream()
	p.emitSnapshot()
	p.emitHeartbeat()

	if len(pub.events()) != 0 {
		t.Fatal("a full publisher recorded events")
	}
	// The sequence still advanced, which is exactly what leaves the detectable gap.
	if p.seq.Load() != 1 {
		t.Fatalf("seq = %d, want 1 — a dropped delta still consumes its number", p.seq.Load())
	}
}

// publisher is nil-able to disable the event log entirely, so every emit path has to tolerate it.
// A nil-pointer panic here would take the matcher goroutine down with it.
func TestANilPublisherDisablesEmissionSafely(t *testing.T) {
	p := newStreamProcessor(t, nil)

	restOrder(t, p, oeq.BuyOrder, 100, 10)
	p.publishStream()
	p.emitSnapshot()
	p.emitHeartbeat()
	p.publishOrderUpdate(uuid.New(), uuid.New(), "dead_lettered", 0, 0)

	if p.seq.Load() != 0 {
		t.Fatalf("seq = %d, want 0 — nothing is sequenced when emission is off", p.seq.Load())
	}
}

// Draining is destructive: the events a batch accumulated belong to that batch alone. Publishing
// them twice would replay trades onto the tape.
func TestDrainedEventsAreNotRepublished(t *testing.T) {
	pub := &recordingPublisher{}
	p := newStreamProcessor(t, pub)

	restOrder(t, p, oeq.BuyOrder, 100, 10)
	p.publishStream()
	after := len(pub.events())

	p.publishStream()

	if got := len(pub.events()); got != after {
		t.Fatalf("a second drain republished %d events", got-after)
	}
}
