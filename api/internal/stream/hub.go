package stream

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
)

// eventBuffer sizes the channel between the subscriber's consume goroutine and the Hub loop. The
// Hub loop is fast (map updates + buffered channel sends), so this rarely fills; if it does, the
// consumer back-pressures briefly — and any genuinely missed delta self-heals via the next snapshot.
const eventBuffer = 4096

// eventSource is the subset of rabbitmq.Subscriber the Hub needs (interface in the consumer, for
// testability). C2 will extend it with BindPattern/UnbindPattern for private per-user bindings.
type eventSource interface {
	Subscribe(ctx context.Context, handler rabbitmq.ExchangeHandler) error
	BindPattern(pattern string) error
	UnbindPattern(pattern string) error
}

// type Envelope interface {
// 	marketdata.Envelope | marketdata.OrderUpdate
// }

type event struct {
	routingKey string
	envelope   marketdata.Envelope
}

// Hub is the single-goroutine owner of every market's book cache and the SSE client registry.
// Routing all three inputs — incoming events, client registrations, client removals — through one
// loop means the cache and registry need no locks, and it removes the snapshot/delta join race:
// a client is registered (and sent its initial snapshot) atomically with respect to delta delivery.
type Hub struct {
	logger        *logger.Logger
	source        eventSource
	caches        map[string]*bookCache                 // market -> cache
	marketClients map[string]map[*marketclient]struct{} // market -> connected clients
	userClients   map[string]map[*userclient]struct{}   // userID -> connected user clients
	events        chan event
	register      chan client
	unregister    chan client
	done          chan struct{}
}

// Run starts the consume goroutine and the actor loop, returning when ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	go h.consume(ctx)
	h.loop(ctx)
}

// consume drains the subscriber, parses each envelope, and hands it to the Hub loop. Parsing here
// (off the loop) keeps the loop cheap; malformed envelopes are dropped.
func (h *Hub) consume(ctx context.Context) {
	err := h.source.Subscribe(ctx, func(d rabbitmq.ExchangeDelivery) {
		env, err := marketdata.ParseEnvelope(d.Body)
		if err != nil {
			h.logger.Error(fmt.Sprintf("market stream: malformed envelope rk=%s: %v", d.RoutingKey, err))
			return
		}
		select {
		case h.events <- event{routingKey: d.RoutingKey, envelope: env}:
		case <-ctx.Done():
		}
	})
	if err != nil {
		h.logger.Error(fmt.Sprintf("market stream subscriber: %v", err))
	}
}

func (h *Hub) loop(ctx context.Context) {
	defer close(h.done)
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return
		case env := <-h.events:
			h.handleEvent(env)
		case c := <-h.register:
			h.handleRegister(c)
		case c := <-h.unregister:
			h.removeClient(c)
		}
	}
}

// handleEvent applies a public event to the market's cache and fans the client-facing frame out.
// Book deltas are forwarded only when they applied in order (the cache stays authoritative);
// trades and heartbeats always forward (the tape is independent of book sync, heartbeats keep SSE
// connections warm). A re-sync (snapshot after an unsynced window) re-broadcasts the full book so
// clients reset. Private order events are handled in C2.
func (h *Hub) handleEvent(e event) {
	env := e.envelope

	// Private order events route by user id (from the routing key), not by market — they must not
	// be gated on a public book cache existing for env.Market.
	if env.Type == marketdata.EventOrder {
		h.handleOrder(e)
		return
	}

	cache := h.caches[env.Market]
	if cache == nil {
		return // an event for a market we do not serve; our bindings should prevent this
	}

	switch env.Type {
	case marketdata.EventSnapshot:
		var s marketdata.Snapshot
		if err := env.Decode(&s); err != nil {
			h.logger.Error(fmt.Sprintf("market stream: decode snapshot %s: %v", env.Market, err))
			return
		}
		wasSynced := cache.synced
		cache.applySnapshot(s)
		if !wasSynced {
			// First sync or recovery after a gap: push a fresh full book so clients reset.
			h.broadcast(env.Market, snapshotFrame(env.Market, cache))
		}

	case marketdata.EventBook:
		var b marketdata.Book
		if err := env.Decode(&b); err != nil {
			h.logger.Error(fmt.Sprintf("market stream: decode book %s: %v", env.Market, err))
			return
		}
		if cache.applyDelta(env.Epoch, env.Seq, b) {
			h.broadcast(env.Market, bookFrame(b))
		}

	case marketdata.EventTrade:
		var t marketdata.Trade
		if err := env.Decode(&t); err != nil {
			h.logger.Error(fmt.Sprintf("market stream: decode trade %s: %v", env.Market, err))
			return
		}
		h.broadcast(env.Market, tradeFrame(t))

	case marketdata.EventHeartbeat:
		cache.checkHeartbeat(env.Epoch, env.Seq)
		h.broadcast(env.Market, heartbeatFrame())
	}
}

// handleOrder routes a private order update to the owning user's connection(s) only. The user id
// lives solely in the routing key (the payload deliberately omits it); isolation is structural —
// the userClients index is keyed by the authenticated uid, so an event can only reach its owner.
func (h *Hub) handleOrder(e event) {
	uid, ok := marketdata.UserIDFromKey(e.routingKey)
	if !ok {
		h.logger.Error(fmt.Sprintf("user stream: not a private routing key: %q", e.routingKey))
		return
	}
	if len(h.userClients[uid]) == 0 {
		return // no connection for this user on this instance
	}
	var u marketdata.OrderUpdate
	if err := e.envelope.Decode(&u); err != nil {
		h.logger.Error(fmt.Sprintf("user stream: decode order for %s: %v", uid, err))
		return
	}
	h.broadcastUserEnv(uid, orderFrame(u))
}

// handleRegister adds a client and immediately sends it the current cached book as its first frame.
// Doing this on the loop guarantees the snapshot reflects exactly the deltas applied so far, and any
// later delta is enqueued strictly after — no duplicate or missed events at the join.
func (h *Hub) handleRegister(c client) {
	switch cl := c.(type) {
	case *marketclient:
		set := h.marketClients[cl.market]
		if set == nil {
			// Unknown market: the handler validates first, so this is defensive. Close to end the stream.
			close(cl.ch)
			return
		}
		set[cl] = struct{}{}
		h.send(cl, snapshotFrame(cl.market, h.caches[cl.market]))
	case *userclient:
		uid := cl.userID.String()
		set := h.userClients[uid]
		// A user may hold several connections (bot + dashboard, reconnects). The binding is
		// ref-counted by len(set): bind only on the first connection for this user.
		if set == nil {
			set = make(map[*userclient]struct{})
			h.userClients[uid] = set
			if err := h.source.BindPattern(marketdata.UserBinding(uid)); err != nil {
				// Without the binding the broker won't route this user's events here, so the stream
				// is useless: drop it (close → reconnect & retry) rather than leave it silently dead.
				h.logger.Error(fmt.Sprintf("couldn't bind user id %s", uid))
				h.logger.ErrorO(err)
				delete(h.userClients, uid)
				close(cl.ch)
				return
			}
		}
		set[cl] = struct{}{}
	}
}

// removeClient drops a client and closes its channel (ending its stream writer). Idempotent: a
// client already gone is skipped, so the close happens exactly once even when both a slow-consumer
// drop and the writer's own disconnect race. Always runs on the Hub goroutine.
func (h *Hub) removeClient(c client) {
	switch cl := c.(type) {
	case *marketclient:
		set := h.marketClients[cl.market]
		if set == nil {
			return
		}
		if _, ok := set[cl]; !ok {
			return
		}
		delete(set, cl)
		close(cl.ch)
	case *userclient:
		uid := cl.userID.String()
		set := h.userClients[uid]
		if set == nil {
			return
		}
		if _, ok := set[cl]; !ok {
			return
		}
		delete(set, cl)
		close(cl.ch)
		// Unbind (and drop the index entry) only once the user's last connection is gone, so the
		// binding is ref-counted by len(set). Dropping the empty entry is what lets the user
		// reconnect (handleRegister treats a missing entry as the first connection).
		if len(set) == 0 {
			delete(h.userClients, uid)
			if err := h.source.UnbindPattern(marketdata.UserBinding(uid)); err != nil {
				h.logger.Error(fmt.Sprintf("couldn't unbind user id %s", uid))
				h.logger.ErrorO(err)
			}
		}
	}
}

// broadcast fans a frame out to every client of a market. A nil frame (impossible marshal error) is
// skipped. Deleting a slow client mid-range is safe in Go.
func (h *Hub) broadcast(market string, frame []byte) {
	if frame == nil {
		return
	}
	for c := range h.marketClients[market] {
		h.send(c, frame)
	}
}

// send enqueues a frame without blocking. If the client's buffer is full it is a slow consumer:
// drop it (close its stream) so it reconnects and re-snapshots rather than stalling the Hub.
func (h *Hub) send(c *marketclient, frame []byte) {
	if frame == nil {
		return
	}
	select {
	case c.ch <- frame:
	default:
		h.removeClient(c)
	}
}

func (h *Hub) broadcastUserEnv(userID string, frame []byte) {
	if frame == nil {
		return
	}
	for c := range h.userClients[userID] {
		select {
		case c.ch <- frame:
		default:
			h.removeClient(c)
		}
	}
}

// closeAll tears down every client on shutdown so their stream writers return promptly.
func (h *Hub) closeAll() {
	for _, set := range h.marketClients {
		for c := range set {
			delete(set, c)
			close(c.ch)
		}
	}
	for _, set := range h.userClients {
		for c := range set {
			delete(set, c)
			close(c.ch)
		}
	}
}

// connect registers a client with the loop, or closes it immediately if the Hub is shutting down.
func (h *Hub) connect(c client) {
	select {
	case h.register <- c:
	case <-h.done:
		close(c.channel())
	}
}

// disconnect removes a client (no-op if the Hub has already stopped).
func (h *Hub) disconnect(c client) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

// NewHub builds the subscriber (binding market.<m>.# for every served market at startup), the per
// market caches, and the empty client registry. Call Run to start consuming and serving.
func NewHub(rmqClient *rabbitmq.RabbitMQClient, markets []string, log *logger.Logger) (*Hub, error) {
	if log == nil {
		panic("logger cannot be nil")
	}
	if rmqClient == nil {
		panic("rabbitMqClient cannot be nil")
	}

	patterns := make([]string, 0, len(markets))
	for _, m := range markets {
		patterns = append(patterns, marketdata.MarketBinding(m))
	}
	sub, err := rabbitmq.NewSubscriber(rmqClient, rabbitmq.SubscriberArgs{
		Exchange:     marketdata.ExchangeName,
		ExchangeKind: rabbitmq.ExchangeKindTopic,
		Patterns:     patterns,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("market stream hub: %w", err)
	}

	h := &Hub{
		logger:        log,
		source:        sub,
		caches:        make(map[string]*bookCache, len(markets)),
		marketClients: make(map[string]map[*marketclient]struct{}, len(markets)),
		userClients:   make(map[string]map[*userclient]struct{}),
		events:        make(chan event, eventBuffer),
		register:      make(chan client),
		unregister:    make(chan client),
		done:          make(chan struct{}),
	}
	for _, m := range markets {
		h.caches[m] = newBookCache()
		h.marketClients[m] = make(map[*marketclient]struct{})
	}
	return h, nil
}
