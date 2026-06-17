package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/google/uuid"
)

const testMarket = "BTC-USDT"

// fakeSource is a stand-in eventSource that records the dynamic bind/unbind calls so tests can
// assert the per-user binding lifecycle without a real broker.
type fakeSource struct {
	binds    []string
	unbinds  []string
	failBind bool // BindPattern returns an error when set
}

func (f *fakeSource) Subscribe(ctx context.Context, _ rabbitmq.ExchangeHandler) error {
	<-ctx.Done()
	return nil
}
func (f *fakeSource) BindPattern(p string) error {
	if f.failBind {
		return errors.New("bind failed")
	}
	f.binds = append(f.binds, p)
	return nil
}
func (f *fakeSource) UnbindPattern(p string) error { f.unbinds = append(f.unbinds, p); return nil }

// newTestHub builds a Hub with its maps seeded but no real subscriber. The tests drive the actor
// methods (handleEvent/handleRegister/removeClient) directly — exactly what the Run loop does, one
// at a time, on a single goroutine — so there is no concurrency to flake on.
func newTestHub(source eventSource, markets ...string) *Hub {
	h := &Hub{
		logger:        logger.NewLogger(logger.Error),
		source:        source,
		caches:        map[string]*bookCache{},
		marketClients: map[string]map[*marketclient]struct{}{},
		userClients:   map[string]map[*userclient]struct{}{},
		events:        make(chan event, eventBuffer),
		register:      make(chan client),
		unregister:    make(chan client),
		done:          make(chan struct{}),
	}
	for _, m := range markets {
		h.caches[m] = newBookCache()
		h.marketClients[m] = map[*marketclient]struct{}{}
	}
	return h
}

func newMarketClient(market string) *marketclient {
	return &marketclient{market: market, ch: make(chan []byte, clientSendBuffer)}
}

func newUserClient(uid uuid.UUID) *userclient {
	return &userclient{userID: uid, ch: make(chan []byte, clientSendBuffer)}
}

func publicEvent(t *testing.T, typ marketdata.EventType, epoch string, seq uint64, payload any) event {
	t.Helper()
	env, err := marketdata.NewEnvelope(epoch, seq, typ, testMarket, 0, payload)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	return event{routingKey: marketdata.PublicKey(testMarket, typ), envelope: env}
}

func orderEvent(t *testing.T, uid uuid.UUID, u marketdata.OrderUpdate) event {
	t.Helper()
	env, err := marketdata.NewEnvelope("e1", 0, marketdata.EventOrder, testMarket, 0, u)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	return event{routingKey: marketdata.PrivateKey(uid.String(), marketdata.EventOrder), envelope: env}
}

func frameType(t *testing.T, frame []byte) string {
	t.Helper()
	payload := bytes.TrimSuffix(bytes.TrimPrefix(frame, []byte("data: ")), []byte("\n\n"))
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("unmarshal frame %q: %v", frame, err)
	}
	s, _ := m["type"].(string)
	return s
}

func recv(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case f := <-ch:
		return f
	default:
		t.Fatal("expected a frame, channel empty")
		return nil
	}
}

func assertEmpty(t *testing.T, ch chan []byte) {
	t.Helper()
	select {
	case f := <-ch:
		t.Fatalf("expected no frame, got %q", f)
	default:
	}
}

// --- public (market) stream ---

// A connecting market client's first frame is a snapshot of the current cached book.
func TestHubMarketRegisterSendsSnapshot(t *testing.T) {
	h := newTestHub(&fakeSource{}, testMarket)
	h.handleEvent(publicEvent(t, marketdata.EventSnapshot, "e1", 5, marketdata.Snapshot{
		Epoch: "e1", Seq: 5, Market: testMarket,
		Bids: []marketdata.BookLevel{{Price: 100, Quantity: 2}},
	}))

	c := newMarketClient(testMarket)
	h.handleRegister(c)

	if got := frameType(t, recv(t, c.ch)); got != "snapshot" {
		t.Fatalf("first frame = %q, want snapshot", got)
	}
}

// In-order book deltas and trades are forwarded; a gapped delta is dropped by the cache.
func TestHubMarketForwardsAndGaps(t *testing.T) {
	h := newTestHub(&fakeSource{}, testMarket)
	h.handleEvent(publicEvent(t, marketdata.EventSnapshot, "e1", 5, marketdata.Snapshot{Epoch: "e1", Seq: 5, Market: testMarket}))
	c := newMarketClient(testMarket)
	h.handleRegister(c)
	recv(t, c.ch) // drop initial snapshot

	h.handleEvent(publicEvent(t, marketdata.EventBook, "e1", 6, marketdata.Book{Side: "buy", Price: 100, Quantity: 3}))
	if got := frameType(t, recv(t, c.ch)); got != "book" {
		t.Fatalf("frame = %q, want book", got)
	}
	h.handleEvent(publicEvent(t, marketdata.EventTrade, "e1", 6, marketdata.Trade{Price: 100, Quantity: 1, TakerSide: "buy"}))
	if got := frameType(t, recv(t, c.ch)); got != "trade" {
		t.Fatalf("frame = %q, want trade", got)
	}
	h.handleEvent(publicEvent(t, marketdata.EventBook, "e1", 8, marketdata.Book{Side: "buy", Price: 100, Quantity: 9})) // skips 7
	assertEmpty(t, c.ch)
}

// A market client whose buffer fills is dropped from the registry and its channel closed.
func TestHubDropsSlowMarketClient(t *testing.T) {
	h := newTestHub(&fakeSource{}, testMarket)
	c := newMarketClient(testMarket)
	h.handleRegister(c)
	recv(t, c.ch) // drain snapshot so the buffer starts empty

	for i := 0; i < clientSendBuffer; i++ {
		h.send(c, bookFrame(marketdata.Book{Side: "buy", Price: 100, Quantity: 1}))
	}
	if _, ok := h.marketClients[testMarket][c]; !ok {
		t.Fatal("client dropped too early")
	}
	h.send(c, bookFrame(marketdata.Book{Side: "buy", Price: 100, Quantity: 1})) // overflow
	if _, ok := h.marketClients[testMarket][c]; ok {
		t.Fatal("slow client should have been dropped")
	}
}

// --- private (user) stream ---

// Registering a user client binds user.<uid>.# exactly once.
func TestHubUserRegisterBinds(t *testing.T) {
	src := &fakeSource{}
	h := newTestHub(src, testMarket)
	uid := uuid.New()
	h.handleRegister(newUserClient(uid))

	if want := marketdata.UserBinding(uid.String()); len(src.binds) != 1 || src.binds[0] != want {
		t.Fatalf("binds = %v, want one %q", src.binds, want)
	}
}

// An order event reaches only the owning user's connection — never another user's.
func TestHubOrderIsolation(t *testing.T) {
	h := newTestHub(&fakeSource{}, testMarket)
	alice, bob := uuid.New(), uuid.New()
	ac, bc := newUserClient(alice), newUserClient(bob)
	h.handleRegister(ac)
	h.handleRegister(bc)

	h.handleEvent(orderEvent(t, alice, marketdata.OrderUpdate{OrderID: "o1", Status: "filled", Filled: 5}))

	if got := frameType(t, recv(t, ac.ch)); got != "order" {
		t.Fatalf("alice frame = %q, want order", got)
	}
	assertEmpty(t, bc.ch) // bob must never see alice's order
}

// An order event for a user with no connection on this instance is a no-op (and does not panic).
func TestHubOrderNoConnection(t *testing.T) {
	h := newTestHub(&fakeSource{}, testMarket)
	h.handleEvent(orderEvent(t, uuid.New(), marketdata.OrderUpdate{OrderID: "o1", Status: "open"}))
}

// A user can reconnect after disconnecting: the binding is unbound on the last disconnect and the
// index entry dropped, so a fresh connection binds again. (Regression test for the empty-map bug.)
func TestHubUserReconnect(t *testing.T) {
	src := &fakeSource{}
	h := newTestHub(src, testMarket)
	uid := uuid.New()

	c1 := newUserClient(uid)
	h.handleRegister(c1)
	h.removeClient(c1)

	if len(src.unbinds) != 1 {
		t.Fatalf("unbinds = %v, want one after last disconnect", src.unbinds)
	}
	if _, ok := h.userClients[uid.String()]; ok {
		t.Fatal("user index entry should be removed after last disconnect")
	}

	c2 := newUserClient(uid)
	h.handleRegister(c2)
	if len(src.binds) != 2 {
		t.Fatalf("binds = %v, want a second bind on reconnect", src.binds)
	}
	if _, ok := h.userClients[uid.String()][c2]; !ok {
		t.Fatal("reconnecting client should be registered")
	}
}

// A user may hold several connections at once (bot + dashboard). All are registered, but the broker
// binding is ref-counted: bound once on the first, and only unbound when the last one leaves.
func TestHubUserMultipleConnections(t *testing.T) {
	src := &fakeSource{}
	h := newTestHub(src, testMarket)
	uid := uuid.New()

	c1, c2 := newUserClient(uid), newUserClient(uid)
	h.handleRegister(c1)
	h.handleRegister(c2)

	if len(h.userClients[uid.String()]) != 2 {
		t.Fatalf("both connections should be registered, got %d", len(h.userClients[uid.String()]))
	}
	if len(src.binds) != 1 {
		t.Fatalf("binds = %v, want exactly one (ref-counted)", src.binds)
	}

	h.removeClient(c1)
	if len(src.unbinds) != 0 {
		t.Fatalf("unbinds = %v, want none while a connection remains", src.unbinds)
	}
	h.removeClient(c2)
	if len(src.unbinds) != 1 {
		t.Fatalf("unbinds = %v, want one after the last connection leaves", src.unbinds)
	}
}

// If the broker bind fails the stream is useless, so the client is dropped (channel closed) and the
// index entry rolled back, leaving no half-registered user.
func TestHubUserBindFailureDropsClient(t *testing.T) {
	h := newTestHub(&fakeSource{failBind: true}, testMarket)
	uid := uuid.New()

	c := newUserClient(uid)
	h.handleRegister(c)

	if _, open := <-c.ch; open {
		t.Fatal("client channel should be closed after a bind failure")
	}
	if _, ok := h.userClients[uid.String()]; ok {
		t.Fatal("failed registration should leave no user index entry")
	}
}
