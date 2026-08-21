package orderbook

import (
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
}

func (ord *Order) canTrade(price uint64) bool {
	if ord.quoteDenom {
		return ord.RemainingQuote >= price // need at least `price` quote to buy one base unit
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

func fillQty(taker, maker *Order, price uint64) uint64 {
	if taker.quoteDenom {
		return min(taker.RemainingQuote/price, maker.Remaining)
	}
	return min(taker.Remaining, maker.Remaining)
}

func takerStatus(t *Order, rests bool) string {
	if t.fullyFilled() {
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

// quoteAmount converts a fill or reservation into quote-quanta.
// Price is in quote-quanta per whole base coin; qty is in base-quanta;
// dividing by baseScale (= 10^baseDecimals) normalises the product.
func quoteAmount(price, qty, baseScale uint64) uint64 {
	return price * qty / baseScale
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
	return limitHaveWant(t.OpenOrder.Side, quoteAmount(t.OpenOrder.Price, t.Remaining, baseScale), t.Remaining)
}
