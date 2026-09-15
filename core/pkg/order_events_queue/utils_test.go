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

func validMarketBuy() *OpenOrderEvent {
	budget := uint64(1000)
	return &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: BuyOrder, Type: MarketOrder, TimeInForce: ImmediateOrCancel,
		QuoteQty: &budget,
	}
}

func validMarketSell() *OpenOrderEvent {
	return &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: SellOrder, Type: MarketOrder, TimeInForce: ImmediateOrCancel,
		Quantity: 10,
	}
}

func ptr[T any](v T) *T { return &v }

// Every order the engine accepts crosses this gate, and it runs twice — once in the API before
// publishing and again in core on delivery. A branch that stops rejecting does not fail loudly: the
// API returns 201 and core dead-letters the command, so the rejections are pinned one by one.
func TestValidateOrderEventRejections(t *testing.T) {
	tests := []struct {
		name        string
		order       func() *OpenOrderEvent
		constraints MarketConstraints
	}{
		{"missing order id", func() *OpenOrderEvent {
			o := validLimit()
			o.OrderID = uuid.UUID{}
			return o
		}, MarketConstraints{}},
		{"missing user id", func() *OpenOrderEvent {
			o := validLimit()
			o.UserID = uuid.UUID{}
			return o
		}, MarketConstraints{}},
		{"unknown side", func() *OpenOrderEvent {
			o := validLimit()
			o.Side = OrderSide("sideways")
			return o
		}, MarketConstraints{}},
		{"empty side", func() *OpenOrderEvent {
			o := validLimit()
			o.Side = ""
			return o
		}, MarketConstraints{}},
		{"unknown type", func() *OpenOrderEvent {
			o := validLimit()
			o.Type = OrderType("stop")
			return o
		}, MarketConstraints{}},
		{"empty type", func() *OpenOrderEvent {
			o := validLimit()
			o.Type = ""
			return o
		}, MarketConstraints{}},
		{"unknown time in force", func() *OpenOrderEvent {
			o := validLimit()
			o.TimeInForce = TimeInForce("day")
			return o
		}, MarketConstraints{}},
		{"empty time in force", func() *OpenOrderEvent {
			o := validLimit()
			o.TimeInForce = ""
			return o
		}, MarketConstraints{}},

		// A market order can never rest, so GTC has no meaning for it.
		{"market order cannot be gtc", func() *OpenOrderEvent {
			o := validMarketSell()
			o.TimeInForce = GoodTillCancel
			return o
		}, MarketConstraints{}},

		{"limit needs a price", func() *OpenOrderEvent {
			o := validLimit()
			o.Price = 0
			return o
		}, MarketConstraints{}},
		{"limit needs a quantity", func() *OpenOrderEvent {
			o := validLimit()
			o.Quantity = 0
			return o
		}, MarketConstraints{}},
		{"limit must not carry a quote budget", func() *OpenOrderEvent {
			o := validLimit()
			o.QuoteQty = ptr(uint64(500))
			return o
		}, MarketConstraints{}},
		// Zero is still "set": the field is a pointer precisely so absence is distinguishable.
		{"limit must not carry a zero quote budget", func() *OpenOrderEvent {
			o := validLimit()
			o.QuoteQty = ptr(uint64(0))
			return o
		}, MarketConstraints{}},

		{"price off the tick", func() *OpenOrderEvent {
			o := validLimit()
			o.Price = 105
			return o
		}, MarketConstraints{PriceQuantum: 10}},
		{"quantity off the lot", func() *OpenOrderEvent {
			o := validLimit()
			o.Quantity = 15
			return o
		}, MarketConstraints{AmountQuantum: 10}},
		{"quantity below the minimum", func() *OpenOrderEvent {
			o := validLimit()
			o.Quantity = 9
			return o
		}, MarketConstraints{MinOrderSize: 10}},
		{"quantity above the maximum", func() *OpenOrderEvent {
			o := validLimit()
			o.Quantity = 11
			return o
		}, MarketConstraints{MaxOrderSize: 10}},

		{"market order must not carry a price", func() *OpenOrderEvent {
			o := validMarketSell()
			o.Price = 100
			return o
		}, MarketConstraints{}},

		// Market orders are denominated by side so the funds to block are known up front. The
		// opposite denomination has no price to convert with, so it is refused rather than guessed.
		{"market buy needs a quote budget", func() *OpenOrderEvent {
			o := validMarketBuy()
			o.QuoteQty = nil
			return o
		}, MarketConstraints{}},
		{"market buy rejects a zero quote budget", func() *OpenOrderEvent {
			o := validMarketBuy()
			o.QuoteQty = ptr(uint64(0))
			return o
		}, MarketConstraints{}},
		{"market buy must not carry a base quantity", func() *OpenOrderEvent {
			o := validMarketBuy()
			o.Quantity = 10
			return o
		}, MarketConstraints{}},
		{"market sell needs a base quantity", func() *OpenOrderEvent {
			o := validMarketSell()
			o.Quantity = 0
			return o
		}, MarketConstraints{}},
		{"market sell must not carry a quote budget", func() *OpenOrderEvent {
			o := validMarketSell()
			o.QuoteQty = ptr(uint64(1000))
			return o
		}, MarketConstraints{}},
		{"market sell respects the lot size", func() *OpenOrderEvent {
			o := validMarketSell()
			o.Quantity = 15
			return o
		}, MarketConstraints{AmountQuantum: 10}},

		// Only a resting order has a lifetime to expire.
		{"expiry on an ioc order", func() *OpenOrderEvent {
			o := validLimit()
			o.TimeInForce, o.ExpiresAt = ImmediateOrCancel, ptr(int64(1757000000))
			return o
		}, MarketConstraints{}},
		{"expiry on a fok order", func() *OpenOrderEvent {
			o := validLimit()
			o.TimeInForce, o.ExpiresAt = FillOrKill, ptr(int64(1757000000))
			return o
		}, MarketConstraints{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateOrderEvent(tt.order(), tt.constraints); !errors.Is(err, ErrInvalidOrderEvent) {
				t.Fatalf("accepted an invalid order, err = %v", err)
			}
		})
	}
}

// The mirror of the rejection table: orders that sit exactly on a boundary, or omit a constraint
// entirely, must pass. Without these a validator that rejected everything would still look correct.
func TestValidateOrderEventAcceptances(t *testing.T) {
	tests := []struct {
		name        string
		order       func() *OpenOrderEvent
		constraints MarketConstraints
	}{
		{"limit gtc", validLimit, MarketConstraints{}},
		{"market buy", validMarketBuy, MarketConstraints{}},
		{"market sell", validMarketSell, MarketConstraints{}},
		{"limit ioc", func() *OpenOrderEvent {
			o := validLimit()
			o.TimeInForce = ImmediateOrCancel
			return o
		}, MarketConstraints{}},
		{"limit fok", func() *OpenOrderEvent {
			o := validLimit()
			o.TimeInForce = FillOrKill
			return o
		}, MarketConstraints{}},
		{"sell side", func() *OpenOrderEvent {
			o := validLimit()
			o.Side = SellOrder
			return o
		}, MarketConstraints{}},
		{"expiry on a gtc order", func() *OpenOrderEvent {
			o := validLimit()
			o.ExpiresAt = ptr(int64(1757000000))
			return o
		}, MarketConstraints{}},
		{"price exactly on the tick", validLimit, MarketConstraints{PriceQuantum: 100}},
		{"quantity exactly on the lot", validLimit, MarketConstraints{AmountQuantum: 10}},
		{"quantity exactly at the minimum", validLimit, MarketConstraints{MinOrderSize: 10}},
		{"quantity exactly at the maximum", validLimit, MarketConstraints{MaxOrderSize: 10}},
		// A zero constraint means unconstrained, not "must equal zero".
		{"zero constraints are unconstrained", validLimit, MarketConstraints{
			PriceQuantum: 0, AmountQuantum: 0, MinOrderSize: 0, MaxOrderSize: 0, BaseScale: 0,
		}},
		// Bounds are expressed in base quanta, and a market buy carries none to check.
		{"market buy ignores base size bounds", validMarketBuy, MarketConstraints{
			AmountQuantum: 7, MinOrderSize: 1000, MaxOrderSize: 2000,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateOrderEvent(tt.order(), tt.constraints); err != nil {
				t.Fatalf("rejected a valid order: %v", err)
			}
		})
	}
}

// A nil event is the one input that is not merely invalid but absent, and it has its own sentinel so
// a caller can tell a programming error from a bad order.
func TestValidateOrderEventRejectsNil(t *testing.T) {
	if err := ValidateOrderEvent(nil, MarketConstraints{}); !errors.Is(err, ErrEmptyOrderEvent) {
		t.Fatalf("err = %v, want ErrEmptyOrderEvent", err)
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
