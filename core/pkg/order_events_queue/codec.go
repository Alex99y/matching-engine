package order_events_queue

import (
	"fmt"

	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	"github.com/google/uuid"
)

// Conversions between the domain types and the generated me.v1 messages. An enum value this build
// does not know decodes to its wire name so that ValidateOrderEvent's rejection names it.

func (o *OrderEvent) toPB() *mev1.OrderCommand {
	command := &mev1.OrderCommand{Version: mev1.Version}
	switch {
	case o.Open != nil:
		command.Command = &mev1.OrderCommand_Open{Open: openToPB(o.Open)}
	case o.Cancel != nil:
		command.Command = &mev1.OrderCommand_Cancel{Cancel: cancelToPB(o.Cancel)}
	}
	return command
}

func fromPB(command *mev1.OrderCommand) (*OrderEvent, error) {
	switch c := command.GetCommand().(type) {
	case *mev1.OrderCommand_Open:
		open, err := openFromPB(c.Open)
		if err != nil {
			return nil, err
		}
		return NewOpenOrderEvent(open), nil
	case *mev1.OrderCommand_Cancel:
		cancel, err := cancelFromPB(c.Cancel)
		if err != nil {
			return nil, err
		}
		return NewCancelOrderEvent(cancel), nil
	default:
		return &OrderEvent{}, nil
	}
}

func openToPB(o *OpenOrderEvent) *mev1.OpenOrder {
	m := &mev1.OpenOrder{
		OrderId:         o.OrderID[:],
		ClientOrderId:   o.ClientOrderID,
		MarketId:        int32(o.MarketID),
		UserId:          o.UserID[:],
		Side:            o.Side.toPB(),
		Type:            o.Type.toPB(),
		TimeInForce:     o.TimeInForce.toPB(),
		Price:           o.Price,
		Quantity:        o.Quantity,
		QuoteQty:        o.QuoteQty,
		ExpiresAt:       o.ExpiresAt,
		PostOnly:        o.PostOnly,
		TakeProfitPrice: o.TakeProfitPrice,
		StopLossPrice:   o.StopLossPrice,
	}
	if o.ParentOrderID != nil {
		m.ParentOrderId = o.ParentOrderID[:]
	}
	return m
}

func openFromPB(m *mev1.OpenOrder) (*OpenOrderEvent, error) {
	orderID, err := uuidFromPB("order_id", m.GetOrderId())
	if err != nil {
		return nil, err
	}
	userID, err := uuidFromPB("user_id", m.GetUserId())
	if err != nil {
		return nil, err
	}
	o := &OpenOrderEvent{
		OrderID:         orderID,
		ClientOrderID:   m.GetClientOrderId(),
		MarketID:        int(m.GetMarketId()),
		UserID:          userID,
		Side:            sideFromPB(m.GetSide()),
		Type:            typeFromPB(m.GetType()),
		TimeInForce:     timeInForceFromPB(m.GetTimeInForce()),
		Price:           m.GetPrice(),
		Quantity:        m.GetQuantity(),
		QuoteQty:        m.QuoteQty,
		ExpiresAt:       m.ExpiresAt,
		PostOnly:        m.GetPostOnly(),
		TakeProfitPrice: m.TakeProfitPrice,
		StopLossPrice:   m.StopLossPrice,
	}
	if m.ParentOrderId != nil {
		parentID, err := uuidFromPB("parent_order_id", m.GetParentOrderId())
		if err != nil {
			return nil, err
		}
		o.ParentOrderID = &parentID
	}
	return o, nil
}

func cancelToPB(c *CancelOrderEvent) *mev1.CancelOrder {
	return &mev1.CancelOrder{OrderId: c.OrderID[:], MarketRef: c.MarketRef}
}

func cancelFromPB(m *mev1.CancelOrder) (*CancelOrderEvent, error) {
	orderID, err := uuidFromPB("order_id", m.GetOrderId())
	if err != nil {
		return nil, err
	}
	return &CancelOrderEvent{OrderID: orderID, MarketRef: m.GetMarketRef()}, nil
}

// uuidFromPB treats an absent id as the nil UUID, which the validator rejects as "required" —
// the same verdict a JSON body without the field used to get. Any other length is not a UUID.
func uuidFromPB(field string, b []byte) (uuid.UUID, error) {
	if len(b) == 0 {
		return uuid.Nil, nil
	}
	id, err := uuid.FromBytes(b)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %s: %w", ErrParsingOrderEvent, field, err)
	}
	return id, nil
}

func (s OrderSide) toPB() mev1.OrderSide {
	switch s {
	case BuyOrder:
		return mev1.OrderSide_ORDER_SIDE_BUY
	case SellOrder:
		return mev1.OrderSide_ORDER_SIDE_SELL
	}
	return mev1.OrderSide_ORDER_SIDE_UNSPECIFIED
}

func sideFromPB(s mev1.OrderSide) OrderSide {
	switch s {
	case mev1.OrderSide_ORDER_SIDE_BUY:
		return BuyOrder
	case mev1.OrderSide_ORDER_SIDE_SELL:
		return SellOrder
	}
	return OrderSide(s.String())
}

func (t OrderType) toPB() mev1.OrderType {
	switch t {
	case LimitOrder:
		return mev1.OrderType_ORDER_TYPE_LIMIT
	case MarketOrder:
		return mev1.OrderType_ORDER_TYPE_MARKET
	}
	return mev1.OrderType_ORDER_TYPE_UNSPECIFIED
}

func typeFromPB(t mev1.OrderType) OrderType {
	switch t {
	case mev1.OrderType_ORDER_TYPE_LIMIT:
		return LimitOrder
	case mev1.OrderType_ORDER_TYPE_MARKET:
		return MarketOrder
	}
	return OrderType(t.String())
}

func (t TimeInForce) toPB() mev1.TimeInForce {
	switch t {
	case GoodTillCancel:
		return mev1.TimeInForce_TIME_IN_FORCE_GTC
	case ImmediateOrCancel:
		return mev1.TimeInForce_TIME_IN_FORCE_IOC
	case FillOrKill:
		return mev1.TimeInForce_TIME_IN_FORCE_FOK
	}
	return mev1.TimeInForce_TIME_IN_FORCE_UNSPECIFIED
}

func timeInForceFromPB(t mev1.TimeInForce) TimeInForce {
	switch t {
	case mev1.TimeInForce_TIME_IN_FORCE_GTC:
		return GoodTillCancel
	case mev1.TimeInForce_TIME_IN_FORCE_IOC:
		return ImmediateOrCancel
	case mev1.TimeInForce_TIME_IN_FORCE_FOK:
		return FillOrKill
	}
	return TimeInForce(t.String())
}
