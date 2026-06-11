package order_events_queue

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrParsingOrderEvent = errors.New("parse order event: invalid payload")
	ErrInvalidOrderEvent = errors.New("invalid order event")
)

type EventType string

const (
	EventTypeOpenOrder   EventType = "open_order"
	EventTypeCancelOrder EventType = "cancel_order"
)

type OrderSide string
type OrderType string
type TimeInForce string

const (
	// Order side
	SellOrder OrderSide = "sell"
	BuyOrder  OrderSide = "buy"

	// Order type
	LimitOrder  OrderType = "limit"
	MarketOrder OrderType = "market"

	// Order time in force
	GoodTillCancel    TimeInForce = "gtc"
	ImmediateOrCancel TimeInForce = "ioc"
	FillOrKill        TimeInForce = "fok"
)

// OrderEvent is the envelope for all events published to the order queues.
// The ME checks Type before decoding Payload into the concrete event struct.
type OrderEvent struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// OpenOrderEvent carries all fields needed to place a new order in the book.
type OpenOrderEvent struct {
	OrderID       uuid.UUID   `json:"order_id"`
	ClientOrderID string      `json:"client_order_id"`
	MarketID      int         `json:"market_id"`
	UserID        uuid.UUID   `json:"user_id"`
	Side          OrderSide   `json:"side"`
	Type          OrderType   `json:"type"`
	TimeInForce   TimeInForce `json:"time_in_force"`
	Price         uint64      `json:"price"`
	Quantity      uint64      `json:"quantity"`
	QuoteQty      *uint64     `json:"quote_qty,omitempty"`
	ExpiresAt     *int64      `json:"expires_at,omitempty"`
}

// CancelOrderEvent requests cancellation of an existing open order.
// MarketRef is used by the publisher to route the event to the correct queue.
type CancelOrderEvent struct {
	OrderID   uuid.UUID `json:"order_id"`
	MarketRef string    `json:"market_ref"`
}

func NewOpenOrderEvent(open *OpenOrderEvent) (*OrderEvent, error) {
	payload, err := json.Marshal(open)
	if err != nil {
		return nil, fmt.Errorf("marshal open order event: %w", err)
	}
	return &OrderEvent{Type: EventTypeOpenOrder, Payload: payload}, nil
}

func NewCancelOrderEvent(cancel *CancelOrderEvent) (*OrderEvent, error) {
	payload, err := json.Marshal(cancel)
	if err != nil {
		return nil, fmt.Errorf("marshal cancel order event: %w", err)
	}
	return &OrderEvent{Type: EventTypeCancelOrder, Payload: payload}, nil
}

func (o *OrderEvent) DecodeOpenOrder() (*OpenOrderEvent, error) {
	var event OpenOrderEvent
	if err := json.Unmarshal(o.Payload, &event); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	return &event, nil
}

func (o *OrderEvent) DecodeCancelOrder() (*CancelOrderEvent, error) {
	var event CancelOrderEvent
	if err := json.Unmarshal(o.Payload, &event); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	return &event, nil
}

// Replace with protobuf or easyjson
func (o *OrderEvent) ToBytes() ([]byte, error) {
	return json.Marshal(o)
}

func ParseOrderEvent(raw []byte) (*OrderEvent, error) {
	event := &OrderEvent{}
	if err := json.Unmarshal(raw, event); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	return event, nil
}
