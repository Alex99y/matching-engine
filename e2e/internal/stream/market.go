package stream

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Frame types on the public market stream. A subscriber receives a snapshot first, then
// deltas; the internal (epoch, seq) the API and core use to keep caches in sync is
// deliberately not on the wire, so a client resynchronises by reconnecting for a new
// snapshot rather than by tracking a sequence number.
const (
	EventSnapshot  = "snapshot"
	EventBook      = "book"
	EventTrade     = "trade"
	EventHeartbeat = "heartbeat"
)

type Level struct {
	Price    uint64
	Quantity uint64
}

type Snapshot struct {
	Market string
	Bids   []Level // best first: high → low
	Asks   []Level // best first: low → high
}

// BookDelta is one price level's new aggregate quantity. Quantity 0 means the level emptied.
type BookDelta struct {
	Side     string
	Price    uint64
	Quantity uint64
}

type Trade struct {
	Price     uint64
	Quantity  uint64
	TakerSide string
}

// MarketEvent is the union of everything the public stream carries. Exactly one of the
// pointers is set, matching Type.
type MarketEvent struct {
	Type     string
	Snapshot *Snapshot
	Book     *BookDelta
	Trade    *Trade
}

// MarketStream is a live subscriber to GET /stream/{market}. group buckets prices the same
// way the depth endpoint's ?group does; 0 leaves levels ungrouped.
type MarketStream struct{ *sub[MarketEvent] }

func ConnectMarket(ctx context.Context, apiURL, marketRef string, group uint64) (*MarketStream, error) {
	url := strings.TrimRight(apiURL, "/") + "/stream/" + marketRef
	if group > 0 {
		url += "?group=" + strconv.FormatUint(group, 10)
	}

	s, err := subscribe(ctx, url, "", decodeMarket)
	if err != nil {
		return nil, err
	}
	return &MarketStream{s}, nil
}

type wireLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

func decodeMarket(raw []byte) (MarketEvent, bool, error) {
	kind, err := frameType(raw)
	if err != nil {
		return MarketEvent{}, false, err
	}

	switch kind {
	case EventSnapshot:
		var w struct {
			Market string      `json:"market"`
			Bids   []wireLevel `json:"bids"`
			Asks   []wireLevel `json:"asks"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return MarketEvent{}, false, err
		}
		return MarketEvent{Type: kind, Snapshot: &Snapshot{
			Market: w.Market, Bids: levels(w.Bids), Asks: levels(w.Asks),
		}}, true, nil

	case EventBook:
		var w struct {
			Side     string `json:"side"`
			Price    string `json:"price"`
			Quantity string `json:"quantity"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return MarketEvent{}, false, err
		}
		return MarketEvent{Type: kind, Book: &BookDelta{
			Side: w.Side, Price: amount(w.Price), Quantity: amount(w.Quantity),
		}}, true, nil

	case EventTrade:
		var w struct {
			Price     string `json:"price"`
			Quantity  string `json:"quantity"`
			TakerSide string `json:"taker_side"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return MarketEvent{}, false, err
		}
		return MarketEvent{Type: kind, Trade: &Trade{
			Price: amount(w.Price), Quantity: amount(w.Quantity), TakerSide: w.TakerSide,
		}}, true, nil

	case EventHeartbeat:
		return MarketEvent{Type: kind}, true, nil
	}
	return MarketEvent{}, false, nil
}

func levels(in []wireLevel) []Level {
	out := make([]Level, len(in))
	for i, l := range in {
		out[i] = Level{Price: amount(l.Price), Quantity: amount(l.Quantity)}
	}
	return out
}

// WaitForSnapshot returns the next snapshot frame.
func (s *MarketStream) WaitForSnapshot(ctx context.Context) (Snapshot, error) {
	ev, err := s.waitFor(ctx, func(e MarketEvent) bool { return e.Type == EventSnapshot })
	if err != nil {
		return Snapshot{}, err
	}
	return *ev.Snapshot, nil
}

// WaitForTrade returns the next trade at price.
func (s *MarketStream) WaitForTrade(ctx context.Context, price uint64) (Trade, error) {
	ev, err := s.waitFor(ctx, func(e MarketEvent) bool {
		return e.Type == EventTrade && e.Trade.Price == price
	})
	if err != nil {
		return Trade{}, err
	}
	return *ev.Trade, nil
}

// WaitForBook returns the next book delta reporting quantity at price on side.
func (s *MarketStream) WaitForBook(ctx context.Context, side string, price, quantity uint64) (BookDelta, error) {
	ev, err := s.waitFor(ctx, func(e MarketEvent) bool {
		return e.Type == EventBook && e.Book.Side == side &&
			e.Book.Price == price && e.Book.Quantity == quantity
	})
	if err != nil {
		return BookDelta{}, err
	}
	return *ev.Book, nil
}

func (s *MarketStream) waitFor(ctx context.Context, match func(MarketEvent) bool) (MarketEvent, error) {
	for {
		ev, err := s.Next(ctx)
		if err != nil {
			return MarketEvent{}, err
		}
		if match(ev) {
			return ev, nil
		}
	}
}
