package order_events_queue

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrEmptyOrderEvent = errors.New("order event cannot be nil")
	ErrInvalidMarketID = errors.New("invalid market id")
)

// MarketConstraints holds the per-market rules used to validate incoming orders.
// The caller (API service) builds this from repository.Market and passes it in —
// no I/O happens inside ValidateOrderEvent.
type MarketConstraints struct {
	PriceQuantum  uint64 // minimum price increment (tick size); 0 = unconstrained
	AmountQuantum uint64 // minimum quantity increment (lot size); 0 = unconstrained
	MinOrderSize  uint64 // minimum base quantity; 0 = unconstrained
	MaxOrderSize  uint64 // maximum base quantity; 0 = unconstrained
}

// ValidateOrderEvent checks structural correctness, market availability, and market
// constraints. availableMarkets must contain an entry for every active market;
// the key must match the MarketID used in OrderEvent.
func ValidateOrderEvent(order *OrderEvent, constraints MarketConstraints) error {
	if order == nil {
		return ErrEmptyOrderEvent
	}

	// Identity fields
	if order.OrderID == (uuid.UUID{}) {
		return fmt.Errorf("%w: order_id is required", ErrInvalidOrderEvent)
	}
	if order.UserID == (uuid.UUID{}) {
		return fmt.Errorf("%w: user_id is required", ErrInvalidOrderEvent)
	}

	// Enum fields
	if order.Side != BuyOrder && order.Side != SellOrder {
		return fmt.Errorf("%w: unknown side %q", ErrInvalidOrderEvent, order.Side)
	}
	if order.Type != LimitOrder && order.Type != MarketOrder {
		return fmt.Errorf("%w: unknown type %q", ErrInvalidOrderEvent, order.Type)
	}
	switch order.TimeInForce {
	case GoodTillCancel, ImmediateOrCancel, FillOrKill:
	default:
		return fmt.Errorf("%w: unknown time_in_force %q", ErrInvalidOrderEvent, order.TimeInForce)
	}

	// Invalid type + TIF combination
	if order.Type == MarketOrder && order.TimeInForce == GoodTillCancel {
		return fmt.Errorf("%w: market orders cannot be GoodTillCancel", ErrInvalidOrderEvent)
	}

	// Price, quantity, and market constraint rules per order type
	switch order.Type {
	case LimitOrder:
		if order.Price == 0 {
			return fmt.Errorf("%w: limit orders require a non-zero price", ErrInvalidOrderEvent)
		}
		if order.Quantity == 0 {
			return fmt.Errorf("%w: limit orders require a non-zero quantity", ErrInvalidOrderEvent)
		}
		if order.QuoteQty != nil {
			return fmt.Errorf("%w: limit orders must not set quote_qty", ErrInvalidOrderEvent)
		}
		if constraints.PriceQuantum > 0 && order.Price%constraints.PriceQuantum != 0 {
			return fmt.Errorf("%w: price %d is not a multiple of tick size %d",
				ErrInvalidOrderEvent, order.Price, constraints.PriceQuantum)
		}
		if err := validateQuantityConstraints(order.Quantity, constraints); err != nil {
			return err
		}

	case MarketOrder:
		if order.Price != 0 {
			return fmt.Errorf("%w: market orders must not set a price", ErrInvalidOrderEvent)
		}
		hasQty := order.Quantity > 0
		hasQuoteQty := order.QuoteQty != nil && *order.QuoteQty > 0
		if !hasQty && !hasQuoteQty {
			return fmt.Errorf("%w: market orders require quantity or quote_qty", ErrInvalidOrderEvent)
		}
		if hasQty && hasQuoteQty {
			return fmt.Errorf("%w: market orders must set quantity or quote_qty, not both", ErrInvalidOrderEvent)
		}
		// Quantity-based market order: validate lot size and order size bounds.
		// Quote-based market order: execution price is unknown upfront, so quantity
		// constraints cannot be applied without the current market price.
		if hasQty {
			if err := validateQuantityConstraints(order.Quantity, constraints); err != nil {
				return err
			}
		}
	}

	// ExpiresAt is only meaningful for GTC orders
	if order.ExpiresAt != nil && order.TimeInForce != GoodTillCancel {
		return fmt.Errorf("%w: expires_at is only valid for GoodTillCancel orders", ErrInvalidOrderEvent)
	}

	return nil
}

func validateQuantityConstraints(quantity uint64, c MarketConstraints) error {
	if c.AmountQuantum > 0 && quantity%c.AmountQuantum != 0 {
		return fmt.Errorf("%w: quantity %d is not a multiple of lot size %d",
			ErrInvalidOrderEvent, quantity, c.AmountQuantum)
	}
	if c.MinOrderSize > 0 && quantity < c.MinOrderSize {
		return fmt.Errorf("%w: quantity %d is below minimum order size %d",
			ErrInvalidOrderEvent, quantity, c.MinOrderSize)
	}
	if c.MaxOrderSize > 0 && quantity > c.MaxOrderSize {
		return fmt.Errorf("%w: quantity %d exceeds maximum order size %d",
			ErrInvalidOrderEvent, quantity, c.MaxOrderSize)
	}
	return nil
}
