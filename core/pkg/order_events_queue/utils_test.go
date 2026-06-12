package order_events_queue

import (
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
)

func validLimit() *OpenOrderEvent {
	return &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: BuyOrder, Type: LimitOrder, TimeInForce: GoodTillCancel,
		Price: 100, Quantity: 10,
	}
}

// Quantities, prices, and the derived notional must fit the BIGINT they are stored in,
// even when each field is individually valid.
func TestStorableOverflow(t *testing.T) {
	none := MarketConstraints{}

	if err := ValidateOrderEvent(validLimit(), none); err != nil {
		t.Fatalf("valid limit rejected: %v", err)
	}

	// notional exactly at the int64 max is allowed
	ok := validLimit()
	ok.Quantity, ok.Price = 2, uint64(math.MaxInt64)/2
	if err := ValidateOrderEvent(ok, none); err != nil {
		t.Fatalf("max-notional order rejected: %v", err)
	}

	// notional one unit over int64 max is rejected
	bad := validLimit()
	bad.Quantity, bad.Price = 2, uint64(math.MaxInt64)/2+1
	if err := ValidateOrderEvent(bad, none); !errors.Is(err, ErrInvalidOrderEvent) {
		t.Fatalf("overflow notional accepted: %v", err)
	}

	// market buy with an overflowing quote budget is rejected
	budget := uint64(math.MaxInt64) + 1
	mb := &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(),
		Side: BuyOrder, Type: MarketOrder, TimeInForce: ImmediateOrCancel, QuoteQty: &budget,
	}
	if err := ValidateOrderEvent(mb, none); !errors.Is(err, ErrInvalidOrderEvent) {
		t.Fatalf("overflow quote_qty accepted: %v", err)
	}

	// market sell with an overflowing base quantity is rejected
	ms := &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(),
		Side: SellOrder, Type: MarketOrder, TimeInForce: ImmediateOrCancel, Quantity: uint64(math.MaxInt64) + 1,
	}
	if err := ValidateOrderEvent(ms, none); !errors.Is(err, ErrInvalidOrderEvent) {
		t.Fatalf("overflow market-sell quantity accepted: %v", err)
	}
}
