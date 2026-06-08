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

type OrderEvent struct {
	OrderID       uuid.UUID   `json:"order_id"`
	ClientOrderID string      `json:"client_order_id"`
	MarketID      int         `json:"market_id"` // Market BD id
	UserID        uuid.UUID   `json:"user_id"`
	Side          OrderSide   `json:"side"`
	Type          OrderType   `json:"type"`
	TimeInForce   TimeInForce `json:"time_in_force"`
	Price         uint64      `json:"price"`                // 0 for market orders
	Quantity      uint64      `json:"quantity"`             // base instrument units
	QuoteQty      *uint64     `json:"quote_qty,omitempty"`  // value-based market orders only
	ExpiresAt     *int64      `json:"expires_at,omitempty"` // Unix seconds, GTC only
}

// Replace with protobuf or easyjson
func (o *OrderEvent) ToBytes() ([]byte, error) {
	return json.Marshal(o)
}

type OrderEventConsumeCallback func(order *OrderEvent) error

func ParseOrderEvent(raw []byte) (*OrderEvent, error) {
	order := &OrderEvent{}
	if err := json.Unmarshal(raw, order); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	return order, nil
}
