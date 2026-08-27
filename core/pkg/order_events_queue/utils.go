package order_events_queue

import (
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/google/uuid"
)

var (
	ErrEmptyOrderEvent = errors.New("order event cannot be nil")
	ErrInvalidMarketID = errors.New("invalid market id")
)

// maxStorableAmount is the largest value a BIGINT column can hold. Any quantity, price,
// quote budget, or derived notional persisted by the engine must fit within it.
const maxStorableAmount uint64 = math.MaxInt64

// MarketConstraints holds the per-market rules used to validate incoming orders.
// The caller (API service) builds this from repository.Market and passes it in —
// no I/O happens inside ValidateOrderEvent.
type MarketConstraints struct {
	PriceQuantum  uint64 // minimum price increment (tick size); 0 = unconstrained
	AmountQuantum uint64 // minimum quantity increment (lot size); 0 = unconstrained
	MinOrderSize  uint64 // minimum base quantity; 0 = unconstrained
	MaxOrderSize  uint64 // maximum base quantity; 0 = unconstrained
	// BaseScale is 10^baseDecimals — needed to check the real notional (price * quantity /
	// BaseScale, see quoteAmount in core/internal/orderbook/order.go), not the unscaled
	// product. 0 is treated as 1 (unscaled); every real market has BaseScale >= 1.
	BaseScale uint64
}

// notionalOverflows reports whether a limit order's real notional — price * quantity /
// baseScale, what actually gets reserved and persisted — exceeds maxStorableAmount. The
// unscaled price * quantity can overflow uint64 long before the scaled-down notional does
// (baseScale is 10^baseDecimals, e.g. 10^9 for a 9-decimal asset), so this uses a 128-bit
// intermediate (bits.Mul64/Div64) instead of a bare multiply — same pattern as feeOf in
// core/internal/orderbook/orderbook.go.
func notionalOverflows(price, quantity, baseScale uint64) bool {
	if baseScale == 0 {
		baseScale = 1
	}
	hi, lo := bits.Mul64(price, quantity)
	if hi >= baseScale {
		return true // quotient would not fit in 64 bits, let alone under maxStorableAmount
	}
	notional, _ := bits.Div64(hi, lo, baseScale)
	return notional > maxStorableAmount
}

// ValidateOrderEvent checks structural correctness, market availability, and market
// constraints. availableMarkets must contain an entry for every active market;
// the key must match the MarketID used in OrderEvent.
func ValidateOrderEvent(order *OpenOrderEvent, constraints MarketConstraints) error {
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

	// Post-only is a resting-only modifier: the order must be able to sit in the book, so
	// it is confined to limit GTC. The non-zero price it also needs is enforced by the
	// LimitOrder rules below.
	if order.PostOnly && (order.Type != LimitOrder || order.TimeInForce != GoodTillCancel) {
		return fmt.Errorf("%w: post_only requires a limit GTC order", ErrInvalidOrderEvent)
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
		// The notional (price × quantity ÷ BaseScale) is persisted as a BIGINT and used for
		// the reservation; reject orders whose real notional overflows it even though price
		// and quantity are each individually valid.
		if notionalOverflows(order.Price, order.Quantity, constraints.BaseScale) {
			return fmt.Errorf("%w: notional price*quantity/base_scale overflows storable maximum (price %d, quantity %d, base_scale %d)",
				ErrInvalidOrderEvent, order.Price, order.Quantity, constraints.BaseScale)
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
		// Market orders are denominated by side so the funds to block are always
		// computable up front: a buy spends a known quote budget (quote_qty); a sell
		// offers a known base quantity. The opposite denomination has an unknown cost
		// (no price to convert with) and is rejected.
		hasQty := order.Quantity > 0
		hasQuoteQty := order.QuoteQty != nil && *order.QuoteQty > 0
		switch order.Side {
		case BuyOrder:
			if !hasQuoteQty {
				return fmt.Errorf("%w: market buy orders require quote_qty", ErrInvalidOrderEvent)
			}
			if hasQty {
				return fmt.Errorf("%w: market buy orders must not set quantity, only quote_qty", ErrInvalidOrderEvent)
			}
			// quote_qty is persisted/reserved as a BIGINT.
			if *order.QuoteQty > maxStorableAmount {
				return fmt.Errorf("%w: quote_qty %d overflows storable maximum", ErrInvalidOrderEvent, *order.QuoteQty)
			}
		case SellOrder:
			if !hasQty {
				return fmt.Errorf("%w: market sell orders require quantity", ErrInvalidOrderEvent)
			}
			if hasQuoteQty {
				return fmt.Errorf("%w: market sell orders must not set quote_qty, only quantity", ErrInvalidOrderEvent)
			}
			// Base quantity is persisted/reserved as a BIGINT.
			if order.Quantity > maxStorableAmount {
				return fmt.Errorf("%w: quantity %d overflows storable maximum", ErrInvalidOrderEvent, order.Quantity)
			}
			// Execution price is unknown for a quote-based order, so lot/size bounds
			// only apply to the base quantity of a sell.
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
