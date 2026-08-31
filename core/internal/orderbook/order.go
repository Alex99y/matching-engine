package orderbook

import (
	"math"
	"math/bits"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

// This file holds the Order type — the engine's in-flight view of one order and its
// fill/reserve bookkeeping — plus the pure arithmetic (unit conversion, have/want mapping,
// status derivation) used to build and settle it. No book/tree access lives here.

// Order is the engine's view of a single order while it lives in or passes through
// the book. Quantities are in indivisible base units except for a market buy, which
// is quote-denominated (a spend budget) — see quoteDenom.
type Order struct {
	OpenOrder *oeq.OpenOrderEvent

	// Remaining is the unfilled base quantity for base-denominated orders (limit
	// orders and market sells). Unused when quoteDenom is true.
	Remaining uint64

	// RemainingQuote is the unspent quote budget for a quote-denominated market buy.
	// Only meaningful when quoteDenom is true.
	RemainingQuote uint64
	quoteDenom     bool

	// reserve is the `have` amount blocked for this order at entry (quote for a buy,
	// base for a sell). Used to compute the over-reservation release at completion.
	reserve uint64

	// Running taker totals across the fills of one MatchOrder call.
	filledBase uint64 // total base traded
	spentQuote uint64 // total quote traded
	// lastPrice is the price of the most recent fill — the prevailing price against which a
	// leftover quote budget is judged spendable or dust (see OrderBook.takerFilled).
	lastPrice uint64
}

func (ord *Order) canTrade(price, baseScale uint64) bool {
	if ord.quoteDenom {
		return affordableBase(ord.RemainingQuote, price, baseScale) > 0
	}
	return ord.Remaining > 0
}

func (ord *Order) stillActive() bool {
	if ord.quoteDenom {
		return ord.RemainingQuote > 0
	}
	return ord.Remaining > 0
}

func (ord *Order) applyFill(qty, price, baseScale uint64) {
	ord.lastPrice = price
	ord.filledBase += qty
	qAmt := quoteAmount(price, qty, baseScale)
	ord.spentQuote += qAmt
	if ord.quoteDenom {
		ord.RemainingQuote -= qAmt
	} else {
		ord.Remaining -= qty
	}
}

func (ord *Order) fullyFilled() bool {
	if ord.quoteDenom {
		return ord.RemainingQuote == 0
	}
	return ord.Remaining == 0
}

func newOrder(event *oeq.OpenOrderEvent, baseScale uint64) *Order {
	ord := &Order{OpenOrder: event}
	if event.Type == oeq.MarketOrder && event.Side == oeq.BuyOrder {
		// Quote-denominated: the order carries a spend budget, not a base quantity.
		ord.quoteDenom = true
		if event.QuoteQty != nil {
			ord.RemainingQuote = *event.QuoteQty
		}
		ord.reserve = ord.RemainingQuote
		return ord
	}

	ord.Remaining = event.Quantity
	if event.Side == oeq.BuyOrder {
		ord.reserve = quoteAmount(event.Price, event.Quantity, baseScale)
	} else {
		ord.reserve = event.Quantity // sell (limit or market): blocks base
	}
	return ord
}

func guardsOK(t *Order) bool {
	if t.quoteDenom {
		return t.RemainingQuote > 0
	}
	if t.Remaining == 0 {
		return false
	}
	if t.OpenOrder.Type == oeq.LimitOrder && t.OpenOrder.Price == 0 {
		return false
	}
	return true
}

func fillQty(taker, maker *Order, price, baseScale uint64) uint64 {
	if taker.quoteDenom {
		return min(affordableBase(taker.RemainingQuote, price, baseScale), maker.Remaining)
	}
	return min(taker.Remaining, maker.Remaining)
}

// takerStatus derives the taker's terminal status. filled is the caller's verdict on
// completion — an exact fill, or a quote-denominated market buy that spent its budget down
// to unspendable dust (see OrderBook.takerFilled) — since fullyFilled alone almost never
// holds for a market buy.
func takerStatus(t *Order, rests, filled bool) string {
	if filled {
		return repository.OrderStatusFilled
	}
	if rests {
		return repository.OrderStatusOpen
	}
	if t.filledBase > 0 {
		return repository.OrderStatusPartiallyFilled
	}
	return repository.OrderStatusCancelled
}

// quoteAmount converts a fill, a reservation, or a whole price level into quote-quanta.
// Price is in quote-quanta per whole base coin; qty is in base-quanta; dividing by
// baseScale (= 10^baseDecimals) normalises the product. The 128-bit intermediate is not
// optional: price*qty overflows uint64 well before the scaled-down result does, and a
// bare multiply would wrap silently.
func quoteAmount(price, qty, baseScale uint64) uint64 {
	if baseScale == 0 {
		baseScale = 1
	}
	hi, lo := bits.Mul64(price, qty)
	if hi >= baseScale {
		return math.MaxUint64 // already far past any storable amount
	}
	v, _ := bits.Div64(hi, lo, baseScale)
	return v
}

// affordableBase is the inverse of quoteAmount: the base-quanta a quote budget buys at
// price. A market buy is quote-denominated, so this (not a bare quantity) is what caps
// each fill; the 128-bit intermediate keeps budget*baseScale from overflowing uint64.
func affordableBase(quote, price, baseScale uint64) uint64 {
	if price == 0 {
		return 0
	}
	if baseScale == 0 {
		baseScale = 1
	}
	hi, lo := bits.Mul64(quote, baseScale)
	if hi >= price {
		return math.MaxUint64 // quotient exceeds uint64; the maker's size caps it downstream
	}
	base, _ := bits.Div64(hi, lo, price)
	return base
}

// limitHaveWant maps a limit order's notional and remaining base quantity to the
// (have, want) convention used in open_orders / cancelled_orders rows:
//
//	buy:  have = quote (notional), want = base (remaining)
//	sell: have = base  (remaining), want = quote (notional)
func limitHaveWant(side oeq.OrderSide, notional, remaining uint64) (have, want uint64) {
	if side == oeq.BuyOrder {
		return notional, remaining
	}
	return remaining, notional
}

// restingRemaining returns the (have, want) amounts still owed for a resting limit order.
func restingRemaining(t *Order, baseScale uint64) (have, want uint64) {
	return limitHaveWant(t.OpenOrder.Side, quoteAmount(t.OpenOrder.Price, t.Remaining, baseScale), t.Remaining)
}

// canceledRemaining returns the (have, want) amounts of the unfilled portion recorded
// for a killed order. For a market buy only the leftover quote budget is known.
func canceledRemaining(t *Order, baseScale uint64) (have, want uint64) {
	if t.quoteDenom {
		return t.RemainingQuote, 0
	}
	return restingRemaining(t, baseScale)
}
