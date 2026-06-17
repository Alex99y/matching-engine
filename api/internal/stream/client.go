package stream

import (
	"encoding/json"
	"strconv"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/google/uuid"
)

// clientSendBuffer caps how far an SSE client may lag. A client whose buffer fills is dropped (its
// stream is closed) and must reconnect to re-snapshot — one slow consumer never stalls the Hub.
const clientSendBuffer = 256

// client is implemented by every SSE subscriber type. The Hub uses it as the common handle for
// registration, removal, and shutdown — concrete routing (market vs. user) happens via type switch.
type client interface {
	channel() chan []byte
}

// marketclient is one connected SSE subscriber. ch carries pre-serialized SSE frames: the Hub is the sole
// writer and the sole closer of ch (always on the Hub goroutine); the stream writer is the sole
// reader.
type marketclient struct {
	market string
	ch     chan []byte
}

func (c *marketclient) channel() chan []byte { return c.ch }

type userclient struct {
	userID uuid.UUID
	ch     chan []byte
}

func (c *userclient) channel() chan []byte { return c.ch }

// --- client-facing wire format ---
//
// Amounts are encoded as strings: a uint64 quantity/price can exceed JavaScript's safe integer
// range (2^53), so the api edge serializes them as decimal strings. Clients never see the internal
// (epoch, seq) — the cache sync is core↔api only.

type levelJSON struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

type snapshotMsg struct {
	Type   string      `json:"type"` // "snapshot"
	Market string      `json:"market"`
	Bids   []levelJSON `json:"bids"`
	Asks   []levelJSON `json:"asks"`
}

type bookMsg struct {
	Type     string `json:"type"` // "book"
	Side     string `json:"side"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"` // "0" means the level was removed
}

type tradeMsg struct {
	Type      string `json:"type"` // "trade"
	Price     string `json:"price"`
	Quantity  string `json:"quantity"`
	TakerSide string `json:"taker_side"`
}

type heartbeatMsg struct {
	Type string `json:"type"` // "heartbeat"
}

type orderMsg struct {
	Type      string `json:"type"` // "order"
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Filled    string `json:"filled"`
	Remaining string `json:"remaining"`
}

func snapshotFrame(market string, c *bookCache) []byte {
	bids, asks := c.snapshotView()
	return marshalFrame(snapshotMsg{
		Type:   "snapshot",
		Market: market,
		Bids:   levelsJSON(bids),
		Asks:   levelsJSON(asks),
	})
}

func bookFrame(b marketdata.Book) []byte {
	return marshalFrame(bookMsg{Type: "book", Side: b.Side, Price: u64(b.Price), Quantity: u64(b.Quantity)})
}

func tradeFrame(t marketdata.Trade) []byte {
	return marshalFrame(tradeMsg{Type: "trade", Price: u64(t.Price), Quantity: u64(t.Quantity), TakerSide: t.TakerSide})
}

func heartbeatFrame() []byte {
	return marshalFrame(heartbeatMsg{Type: "heartbeat"})
}

func orderFrame(u marketdata.OrderUpdate) []byte {
	return marshalFrame(orderMsg{Type: "order", OrderID: u.OrderID, Status: u.Status, Filled: u64(u.Filled), Remaining: u64(u.Remaining)})
}

func levelsJSON(levels []bookLevel) []levelJSON {
	out := make([]levelJSON, len(levels))
	for i, l := range levels {
		out[i] = levelJSON{Price: u64(l.price), Quantity: u64(l.qty)}
	}
	return out
}

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

// marshalFrame serializes a client DTO into a complete SSE frame ("data: <json>\n\n"). The DTOs are
// plain structs that cannot fail to marshal; on the impossible error it returns nil and the caller
// skips the send.
func marshalFrame(v any) []byte {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	frame := make([]byte, 0, len(payload)+8)
	frame = append(frame, "data: "...)
	frame = append(frame, payload...)
	frame = append(frame, '\n', '\n')
	return frame
}
