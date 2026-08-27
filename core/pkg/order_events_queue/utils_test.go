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

// A limit order's real notional is price * quantity / BaseScale, not the unscaled product — a
// BTC order at a realistic price (9 decimals) must not be rejected just because the unscaled
// product overflows uint64, and the check must still catch a notional that overflows even after
// scaling down.
func TestStorableOverflowRespectsBaseScale(t *testing.T) {
	nineDecimals := MarketConstraints{BaseScale: 1_000_000_000}

	// price 79200 USDT, quantity 0.5 BTC (9 decimals) — notional 39,600 USDT, well within range.
	real := validLimit()
	real.Price, real.Quantity = 79_200_000_000, 500_000_000
	if err := ValidateOrderEvent(real, nineDecimals); err != nil {
		t.Fatalf("realistic BTC order rejected: %v", err)
	}

	// same scale, but a notional that genuinely overflows even after dividing by BaseScale.
	tooBig := validLimit()
	tooBig.Price, tooBig.Quantity = math.MaxUint64, math.MaxUint64
	if err := ValidateOrderEvent(tooBig, nineDecimals); !errors.Is(err, ErrInvalidOrderEvent) {
		t.Fatalf("scaled overflow notional accepted: %v", err)
	}
}

// Post-only may only sit in the book, so it is accepted for a limit GTC order and rejected
// for anything that cannot rest: market orders and non-GTC time-in-force.
func TestPostOnlyRequiresLimitGTC(t *testing.T) {
	none := MarketConstraints{}

	ok := validLimit()
	ok.PostOnly = true
	if err := ValidateOrderEvent(ok, none); err != nil {
		t.Fatalf("post-only limit GTC rejected: %v", err)
	}

	for _, tif := range []TimeInForce{ImmediateOrCancel, FillOrKill} {
		bad := validLimit()
		bad.PostOnly, bad.TimeInForce = true, tif
		if err := ValidateOrderEvent(bad, none); !errors.Is(err, ErrInvalidOrderEvent) {
			t.Fatalf("post-only %s accepted: %v", tif, err)
		}
	}

	budget := uint64(1000)
	marketBuy := &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: BuyOrder, Type: MarketOrder, TimeInForce: ImmediateOrCancel,
		QuoteQty: &budget, PostOnly: true,
	}
	if err := ValidateOrderEvent(marketBuy, none); !errors.Is(err, ErrInvalidOrderEvent) {
		t.Fatalf("post-only market order accepted: %v", err)
	}
}
