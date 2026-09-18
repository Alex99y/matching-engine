// Package marketdata defines the wire contract for the live event-log stream (see
// docs/event-log.md): the event envelope, event types, routing-key helpers, and payload DTOs
// shared by core (publisher) and api (subscriber). The wire schema is me.v1.Event
// (common/proto/me/v1/events.proto); this package converts to and from it, nothing else touches
// the generated types.
package marketdata

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrParsingEnvelope    = errors.New("parse envelope: invalid body")
	ErrUnsupportedVersion = errors.New("unsupported schema version")
)

// ExchangeName is the topic exchange all market-data events flow through. Its kind is a transport
// concern — construct with rabbitmq.ExchangeKindTopic.
const ExchangeName = "me.events"

// EventType is both the discriminator in the envelope and the trailing segment of the routing key,
// so it is kept to a single routing word (no dots).
type EventType string

const (
	EventTrade     EventType = "trade"     // public: a fill on the tape
	EventBook      EventType = "book"      // public: an aggregated L2 level change
	EventHeartbeat EventType = "heartbeat" // public: liveness + sequence checkpoint
	EventSnapshot  EventType = "snapshot"  // public: full L2 book at a sequence point (periodic broadcast)
	EventOrder     EventType = "order"     // private: a user's order lifecycle update
)

// Payload is one of the DTOs below. The set is closed so an Envelope can only ever carry a member
// of the wire's oneof; consumers switch on the concrete type.
type Payload interface {
	eventType() EventType
}

// Envelope wraps every event. (Epoch, Seq) sequence the per-market stream: a consumer applies a
// delta only if Seq == lastApplied+1, and re-synchronises from a snapshot on a Seq gap (missed
// events) or a changed Epoch (core restarted). Type always matches Payload's concrete type.
type Envelope struct {
	Epoch   string
	Seq     uint64
	Type    EventType
	Market  string // empty for private events
	Ts      int64  // unix milliseconds
	Payload Payload
}

// --- payloads ---

// Trade is a public fill on the tape — no identities.
type Trade struct {
	Price     uint64
	Quantity  uint64
	TakerSide string // "buy" | "sell"
}

// Book is an aggregated L2 level change. Quantity == 0 means the level was removed. The book is
// published at native price-level resolution (multiples of the market's price_quantum); coarser
// granularities are bucketed at the api edge, not here.
type Book struct {
	Side     string // "buy" | "sell"
	Price    uint64
	Quantity uint64
}

// OrderUpdate is a private, per-user order lifecycle event (routed by user id, never broadcast).
type OrderUpdate struct {
	OrderID   uuid.UUID
	Status    string // pending | open | filled | partially_filled | cancelled | rejected | expired
	Filled    uint64
	Remaining uint64
}

// Heartbeat has no fields beyond the envelope's (Epoch, Seq); it keeps connections warm and lets
// an idle consumer detect a sequence gap without waiting for the next trade.
type Heartbeat struct{}

type BookLevel struct {
	Price    uint64
	Quantity uint64
}

// Snapshot is the authoritative book state at (Epoch, Seq), served by core from its in-memory book.
// A consumer applies live deltas with Seq > the snapshot's Seq after loading it. Bids are ordered
// high→low, asks low→high.
type Snapshot struct {
	Epoch  string
	Seq    uint64
	Market string
	Bids   []BookLevel
	Asks   []BookLevel
}

func (Trade) eventType() EventType       { return EventTrade }
func (Book) eventType() EventType        { return EventBook }
func (OrderUpdate) eventType() EventType { return EventOrder }
func (Heartbeat) eventType() EventType   { return EventHeartbeat }
func (Snapshot) eventType() EventType    { return EventSnapshot }

// --- routing keys ---
//
// Public:  market.<market>.<type>   e.g. market.BTC-USDT.trade
// Private: user.<user_id>.<type>    e.g. user.<uuid>.order

func PublicKey(market string, t EventType) string  { return "market." + market + "." + string(t) }
func PrivateKey(userID string, t EventType) string { return "user." + userID + "." + string(t) }

// MarketBinding matches every public event for one market; UserBinding every private event for one
// user; TypeBinding one event type across all markets.
func MarketBinding(market string) string { return "market." + market + ".#" }
func UserBinding(userID string) string   { return "user." + userID + ".#" }
func TypeBinding(t EventType) string     { return "market.*." + string(t) }

// UserIDFromKey extracts the user id from a private routing key or binding ("user.<uid>.order",
// "user.<uid>.#"). The uid is a UUID (no dots), so a split on "." always yields it as the second
// segment. Returns ok=false for any non-private key.
func UserIDFromKey(key string) (string, bool) {
	parts := strings.Split(key, ".")
	if len(parts) < 3 || parts[0] != "user" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func NewEnvelope(epoch string, seq uint64, market string, tsMillis int64, payload Payload) Envelope {
	return Envelope{Epoch: epoch, Seq: seq, Type: payload.eventType(), Market: market, Ts: tsMillis, Payload: payload}
}
