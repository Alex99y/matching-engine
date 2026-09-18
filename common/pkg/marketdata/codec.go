package marketdata

import (
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/pb/me"
	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

const (
	sideBuy  = "buy"
	sideSell = "sell"
)

func (e Envelope) ToBytes() ([]byte, error) {
	raw, err := proto.Marshal(e.toPB())
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	return raw, nil
}

// ParseEnvelope returns ErrParsingEnvelope for anything that is not a me.v1 event this build can
// decode: another schema version (wrapping ErrUnsupportedVersion), a payload member it does not
// know, or bytes that are not protobuf.
func ParseEnvelope(raw []byte) (Envelope, error) {
	version, err := me.Version(raw)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: %w", ErrParsingEnvelope, err)
	}
	if version != mev1.Version {
		return Envelope{}, fmt.Errorf("%w: %w %d", ErrParsingEnvelope, ErrUnsupportedVersion, version)
	}
	var event mev1.Event
	if err := proto.Unmarshal(raw, &event); err != nil {
		return Envelope{}, fmt.Errorf("%w: %w", ErrParsingEnvelope, err)
	}
	payload, err := payloadFromPB(&event)
	if err != nil {
		return Envelope{}, err
	}
	return NewEnvelope(event.GetEpoch(), event.GetSeq(), event.GetMarket(), event.GetTs(), payload), nil
}

func (e Envelope) toPB() *mev1.Event {
	event := &mev1.Event{Version: mev1.Version, Epoch: e.Epoch, Seq: e.Seq, Market: e.Market, Ts: e.Ts}
	switch p := e.Payload.(type) {
	case Trade:
		event.Payload = &mev1.Event_Trade{Trade: &mev1.Trade{Price: p.Price, Quantity: p.Quantity, TakerSide: sideToPB(p.TakerSide)}}
	case Book:
		event.Payload = &mev1.Event_Book{Book: &mev1.Book{Side: sideToPB(p.Side), Price: p.Price, Quantity: p.Quantity}}
	case Heartbeat:
		event.Payload = &mev1.Event_Heartbeat{Heartbeat: &mev1.Heartbeat{}}
	case Snapshot:
		event.Payload = &mev1.Event_Snapshot{Snapshot: &mev1.Snapshot{
			Epoch: p.Epoch, Seq: p.Seq, Market: p.Market, Bids: levelsToPB(p.Bids), Asks: levelsToPB(p.Asks),
		}}
	case OrderUpdate:
		event.Payload = &mev1.Event_Order{Order: &mev1.OrderUpdate{
			OrderId: p.OrderID[:], Status: p.Status, Filled: p.Filled, Remaining: p.Remaining,
		}}
	}
	return event
}

func payloadFromPB(event *mev1.Event) (Payload, error) {
	switch p := event.GetPayload().(type) {
	case *mev1.Event_Trade:
		return Trade{Price: p.Trade.GetPrice(), Quantity: p.Trade.GetQuantity(), TakerSide: sideFromPB(p.Trade.GetTakerSide())}, nil
	case *mev1.Event_Book:
		return Book{Side: sideFromPB(p.Book.GetSide()), Price: p.Book.GetPrice(), Quantity: p.Book.GetQuantity()}, nil
	case *mev1.Event_Heartbeat:
		return Heartbeat{}, nil
	case *mev1.Event_Snapshot:
		return Snapshot{
			Epoch:  p.Snapshot.GetEpoch(),
			Seq:    p.Snapshot.GetSeq(),
			Market: p.Snapshot.GetMarket(),
			Bids:   levelsFromPB(p.Snapshot.GetBids()),
			Asks:   levelsFromPB(p.Snapshot.GetAsks()),
		}, nil
	case *mev1.Event_Order:
		orderID, err := uuid.FromBytes(p.Order.GetOrderId())
		if err != nil {
			return nil, fmt.Errorf("%w: order_id: %w", ErrParsingEnvelope, err)
		}
		return OrderUpdate{OrderID: orderID, Status: p.Order.GetStatus(), Filled: p.Order.GetFilled(), Remaining: p.Order.GetRemaining()}, nil
	default:
		return nil, fmt.Errorf("%w: no event this version knows", ErrParsingEnvelope)
	}
}

func levelsToPB(levels []BookLevel) []*mev1.BookLevel {
	out := make([]*mev1.BookLevel, len(levels))
	for i, l := range levels {
		out[i] = &mev1.BookLevel{Price: l.Price, Quantity: l.Quantity}
	}
	return out
}

func levelsFromPB(levels []*mev1.BookLevel) []BookLevel {
	out := make([]BookLevel, len(levels))
	for i, l := range levels {
		out[i] = BookLevel{Price: l.GetPrice(), Quantity: l.GetQuantity()}
	}
	return out
}

func sideToPB(side string) mev1.OrderSide {
	switch side {
	case sideBuy:
		return mev1.OrderSide_ORDER_SIDE_BUY
	case sideSell:
		return mev1.OrderSide_ORDER_SIDE_SELL
	}
	return mev1.OrderSide_ORDER_SIDE_UNSPECIFIED
}

func sideFromPB(side mev1.OrderSide) string {
	switch side {
	case mev1.OrderSide_ORDER_SIDE_BUY:
		return sideBuy
	case mev1.OrderSide_ORDER_SIDE_SELL:
		return sideSell
	}
	return side.String()
}
