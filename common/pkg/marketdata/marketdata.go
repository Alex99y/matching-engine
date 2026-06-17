// Package marketdata defines the wire contract for the live event-log stream (see
// docs/event-log.md): the event envelope, event types, routing-key helpers, and payload DTOs
// shared by core (publisher) and api (subscriber). It is transport-agnostic — serialization only,
// no RabbitMQ or I/O. Amounts stay uint64 on this internal core→api hop; JS-safe (string) encoding
// for client-facing payloads is the api edge's concern.
package marketdata

import (
	"encoding/json"
	"strings"
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

// Envelope wraps every event. (Epoch, Seq) sequence the per-market stream: a consumer applies a
// delta only if Seq == lastApplied+1, and re-synchronises from a snapshot on a Seq gap (missed
// events) or a changed Epoch (core restarted). Payload is the type-specific DTO below.
type Envelope struct {
	Epoch   string          `json:"epoch"`
	Seq     uint64          `json:"seq"`
	Type    EventType       `json:"type"`
	Market  string          `json:"market,omitempty"`
	Ts      int64           `json:"ts"` // unix milliseconds
	Payload json.RawMessage `json:"payload"`
}

// --- payloads ---

// Trade is a public fill on the tape — no identities.
type Trade struct {
	Price     uint64 `json:"price"`
	Quantity  uint64 `json:"quantity"`
	TakerSide string `json:"taker_side"` // "buy" | "sell"
}

// Book is an aggregated L2 level change. Quantity == 0 means the level was removed. The book is
// published at native price-level resolution (multiples of the market's price_quantum); coarser
// granularities are bucketed at the api edge, not here.
type Book struct {
	Side     string `json:"side"` // "buy" | "sell"
	Price    uint64 `json:"price"`
	Quantity uint64 `json:"quantity"`
}

// OrderUpdate is a private, per-user order lifecycle event (routed by user id, never broadcast).
type OrderUpdate struct {
	OrderID   string `json:"order_id"`
	Status    string `json:"status"` // open | filled | partially_filled | cancelled | rejected
	Filled    uint64 `json:"filled"`
	Remaining uint64 `json:"remaining"`
}

// Heartbeat has no fields beyond the envelope's (Epoch, Seq); it keeps connections warm and lets
// an idle consumer detect a sequence gap without waiting for the next trade.
type Heartbeat struct{}

// --- snapshot (RPC reply; full book at a sequence point) ---

type BookLevel struct {
	Price    uint64 `json:"price"`
	Quantity uint64 `json:"quantity"`
}

// Snapshot is the authoritative book state at (Epoch, Seq), served by core from its in-memory book.
// A consumer applies live deltas with Seq > the snapshot's Seq after loading it. Bids are ordered
// high→low, asks low→high.
type Snapshot struct {
	Epoch  string      `json:"epoch"`
	Seq    uint64      `json:"seq"`
	Market string      `json:"market"`
	Bids   []BookLevel `json:"bids"`
	Asks   []BookLevel `json:"asks"`
}

// SnapshotRequest is the RPC request a consumer sends to ask core for a market's current snapshot.
type SnapshotRequest struct {
	Market string `json:"market"`
}

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

// --- envelope helpers ---

// NewEnvelope marshals a payload DTO into a sequenced envelope.
func NewEnvelope(epoch string, seq uint64, t EventType, market string, tsMillis int64, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Epoch: epoch, Seq: seq, Type: t, Market: market, Ts: tsMillis, Payload: raw}, nil
}

func (e Envelope) ToBytes() ([]byte, error) { return json.Marshal(e) }

func ParseEnvelope(b []byte) (Envelope, error) {
	var e Envelope
	err := json.Unmarshal(b, &e)
	return e, err
}

// Decode unmarshals the envelope's payload into out (e.g. a *Trade matching e.Type).
func (e Envelope) Decode(out any) error { return json.Unmarshal(e.Payload, out) }
