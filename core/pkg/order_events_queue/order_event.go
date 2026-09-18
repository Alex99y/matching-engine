package order_events_queue

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/pb/me"
	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	ErrParsingOrderEvent  = errors.New("parse order event: invalid payload")
	ErrInvalidOrderEvent  = errors.New("invalid order event")
	ErrUnsupportedVersion = errors.New("unsupported schema version")
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

// OrderEvent is a decoded command from the order queue (wire schema: me.v1.OrderCommand). Exactly
// one of Open and Cancel is set, matching Type; both are nil and Type is empty when the body
// carried a command this version does not know.
type OrderEvent struct {
	Type   EventType
	Open   *OpenOrderEvent
	Cancel *CancelOrderEvent
}

// OpenOrderEvent carries all fields needed to place a new order in the book.
type OpenOrderEvent struct {
	OrderID         uuid.UUID
	ClientOrderID   string
	MarketID        int
	UserID          uuid.UUID
	Side            OrderSide
	Type            OrderType
	TimeInForce     TimeInForce
	Price           uint64
	Quantity        uint64
	QuoteQty        *uint64
	ExpiresAt       *int64
	PostOnly        bool
	TakeProfitPrice *uint64
	StopLossPrice   *uint64
	ParentOrderID   *uuid.UUID
}

func (o *OpenOrderEvent) HasTriggers() bool {
	return o.TakeProfitPrice != nil || o.StopLossPrice != nil
}

// ExitOrderID derives the bracket exit's id from its entry's, so the API can announce it at
// creation and the engine needs neither a generator nor an extra column to find it again.
func ExitOrderID(entryID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(entryID, []byte("exit"))
}

// CancelOrderEvent requests cancellation of an existing open order.
// MarketRef is used by the publisher to route the event to the correct queue.
type CancelOrderEvent struct {
	OrderID   uuid.UUID
	MarketRef string
}

func NewOpenOrderEvent(open *OpenOrderEvent) *OrderEvent {
	return &OrderEvent{Type: EventTypeOpenOrder, Open: open}
}

func NewCancelOrderEvent(cancel *CancelOrderEvent) *OrderEvent {
	return &OrderEvent{Type: EventTypeCancelOrder, Cancel: cancel}
}

func (o *OrderEvent) ToBytes() ([]byte, error) {
	raw, err := proto.Marshal(o.toPB())
	if err != nil {
		return nil, fmt.Errorf("marshal order event: %w", err)
	}
	return raw, nil
}

// ParseOrderEvent returns ErrParsingOrderEvent for anything that is not a me.v1 command this
// build can decode, including a body of another schema version (wrapping ErrUnsupportedVersion).
func ParseOrderEvent(raw []byte) (*OrderEvent, error) {
	command, err := parseCommand(raw)
	if err != nil {
		return nil, err
	}
	return fromPB(command)
}

// CommandJSON renders a body as an operator reads it (dead_letters.payload). It fails for the
// same bodies ParseOrderEvent does.
func CommandJSON(raw []byte) (json.RawMessage, error) {
	command, err := parseCommand(raw)
	if err != nil {
		return nil, err
	}
	rendered, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	return rendered, nil
}

func parseCommand(raw []byte) (*mev1.OrderCommand, error) {
	version, err := me.Version(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	if version != mev1.Version {
		return nil, fmt.Errorf("%w: %w %d", ErrParsingOrderEvent, ErrUnsupportedVersion, version)
	}
	var command mev1.OrderCommand
	if err := proto.Unmarshal(raw, &command); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsingOrderEvent, err)
	}
	return &command, nil
}
