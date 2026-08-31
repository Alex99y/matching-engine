package orderbook

import (
	"math/bits"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

// This file holds the taker-matching algorithm: crossing an incoming order against the
// resting book, recording fills, and settling the taker's own outcome (rest, cancel, or fill).

// MatchOrder assumes the caller already moved the order's funds balance -> blocked; it
// never reserves funds itself.
func (o *OrderBook) MatchOrder(event *oeq.OpenOrderEvent, result *repository.BatchResult) {
	taker := newOrder(event, o.market.BaseScale)

	// mayRest is cleared by the paths that must not leave the order in the book even
	// though it looks restable: completion then releases the reservation and records a
	// cancellation. A FOK kill needs no such flag — takerRests only admits GTC.
	mayRest := true
	switch {
	case !guardsOK(taker):
		// Defensive: ValidateOrderEvent should already reject these.
		o.logger.Warn("orderbook: order failed defensive guards, rejecting")
		mayRest = false
	case taker.OpenOrder.PostOnly && o.crossesBook(taker):
		// A post-only order that would take liquidity is cancelled untouched — it may
		// only rest, and here it cannot.
		mayRest = false
	case taker.OpenOrder.TimeInForce == oeq.FillOrKill && !o.canFill(taker):
		// FOK that cannot fully fill is killed untouched — skip matching.
	default:
		o.match(taker, result)
	}

	rests := mayRest && o.takerRests(taker)
	filled := o.takerFilled(taker)
	o.settleTakerCompletion(taker, rests, result)
	o.emitTakerOutcome(taker, rests, filled, result)
}

// takerFilled reports whether the taker got everything it could. Beyond an exact fill this
// covers a quote-denominated market buy that spent its budget down to a sub-quantum
// remainder it can no longer trade with, while the book still holds asks it could not
// afford: the dust is refunded at settlement, so the order is filled, not partially filled.
func (o *OrderBook) takerFilled(t *Order) bool {
	if t.fullyFilled() {
		return true
	}
	return t.quoteDenom && t.filledBase > 0 &&
		o.oppositeTree(t.OpenOrder.Side).Len() > 0
}

// crossesBook reports whether the order would trade against the resting book right now.
// Used to reject a post-only order before it can take.
func (o *OrderBook) crossesBook(order *Order) bool {
	crossed := false
	o.eachCrossingLevel(order, func(*PriceLevel) bool {
		crossed = true
		return false
	})
	return crossed
}

// match walks the opposite side price-time-first, filling the taker against each resting
// maker in FIFO order at that price until the taker is done or the level stops crossing.
// Levels that empty out mid-walk are collected and deleted only after Ascend/Descend
// returns — mutating the tree during iteration is unsafe.
func (o *OrderBook) match(taker *Order, result *repository.BatchResult) {
	var emptyLevels []*PriceLevel

	o.eachCrossingLevel(taker, func(lvl *PriceLevel) bool {
		for lvl.Orders.Len() > 0 && taker.canTrade(lvl.Price, o.market.BaseScale) {
			front := lvl.Orders.Front()
			maker, ok := front.Value.(*Order)
			if !ok {
				o.logger.Error("orderbook: corrupt list element in match")
				lvl.Orders.Remove(front)
				continue
			}

			qty := fillQty(taker, maker, lvl.Price, o.market.BaseScale)
			if qty == 0 {
				// Quote-denominated taker can no longer afford a single unit here.
				break
			}

			taker.applyFill(qty, lvl.Price, o.market.BaseScale)
			maker.Remaining -= qty
			lvl.TotalQty -= qty
			o.markLevel(oppositeSide(taker.OpenOrder.Side), lvl.Price)

			o.emitTrade(taker, maker, qty, lvl.Price, result)

			if maker.Remaining == 0 {
				lvl.Orders.Remove(front)
				delete(o.index, maker.OpenOrder.OrderID)
				o.unindexExpiry(maker)
				o.emitMakerFilled(maker, result)
			} else {
				o.emitMakerPartialFill(maker, result)
			}
		}

		if lvl.Orders.Len() == 0 {
			emptyLevels = append(emptyLevels, lvl)
		}

		return taker.stillActive()
	})

	oppTree := o.oppositeTree(taker.OpenOrder.Side)
	for _, lvl := range emptyLevels {
		oppTree.Delete(lvl)
	}
}

// canFill reports whether the crossing liquidity can fully satisfy a FOK taker. A quote-
// denominated market buy needs its budget absorbed, every other order its base quantity, so
// each level is valued in the taker's own denomination and drawn down from what it still needs.
// Draining need (rather than summing supply) keeps the running total from overflowing when a
// deep level's notional saturates.
func (o *OrderBook) canFill(taker *Order) bool {
	need := taker.Remaining
	if taker.quoteDenom {
		need = taker.RemainingQuote
	}

	o.eachCrossingLevel(taker, func(lvl *PriceLevel) bool {
		avail := lvl.TotalQty
		if taker.quoteDenom {
			avail = quoteAmount(lvl.Price, lvl.TotalQty, o.market.BaseScale)
		}
		if avail >= need {
			need = 0
			return false
		}
		need -= avail
		return true
	})
	return need == 0
}

func crosses(order *Order, levelPrice uint64) bool {
	if order.OpenOrder.Type == oeq.MarketOrder {
		return true
	}
	if order.OpenOrder.Side == oeq.BuyOrder {
		return order.OpenOrder.Price >= levelPrice
	}
	return order.OpenOrder.Price <= levelPrice
}

// emitTrade records one fill: settlement movements for both parties and the match row.
// Called after the fill has been applied to both orders.
func (o *OrderBook) emitTrade(taker, maker *Order, qty, price uint64, result *repository.BatchResult) {
	quoteAmt := quoteAmount(price, qty, o.market.BaseScale)

	var buyer, seller *Order
	buyerIsTaker := taker.OpenOrder.Side == oeq.BuyOrder
	if buyerIsTaker {
		buyer, seller = taker, maker
	} else {
		buyer, seller = maker, taker
	}

	// Fees are charged on the asset each party receives (taker/maker rate per role), with
	// no house account — they simply leave the traders' balances. Buyer receives base,
	// seller receives quote.
	buyerFeeBps, sellerFeeBps := o.market.MakerFeeBps, o.market.TakerFeeBps
	if buyerIsTaker {
		buyerFeeBps, sellerFeeBps = o.market.TakerFeeBps, o.market.MakerFeeBps
	}
	buyerFee := feeOf(qty, buyerFeeBps)        // in base
	sellerFee := feeOf(quoteAmt, sellerFeeBps) // in quote

	result.AddBalanceDelta(buyer.OpenOrder.UserID, o.quoteInstr(), 0, -int64(quoteAmt))
	result.AddBalanceDelta(buyer.OpenOrder.UserID, o.baseInstr(), int64(qty-buyerFee), 0)
	result.AddBalanceDelta(seller.OpenOrder.UserID, o.baseInstr(), 0, -int64(qty))
	result.AddBalanceDelta(seller.OpenOrder.UserID, o.quoteInstr(), int64(quoteAmt-sellerFee), 0)

	result.Matches = append(result.Matches, repository.InsertMatchParams{
		MarketID:          o.market.ID,
		BuyOrderID:        buyer.OpenOrder.OrderID,
		SellOrderID:       seller.OpenOrder.OrderID,
		MatchBuyAmount:    qty,      // base bought
		MatchSellAmount:   quoteAmt, // quote sold
		MatchPrice:        price,
		MatchBuyFees:      buyerFee,  // in base
		MatchSellFees:     sellerFee, // in quote
		BuyOrderIsTaker:   buyerIsTaker,
		IsBuyOrderFilled:  buyer.fullyFilled(),
		IsSellOrderFilled: seller.fullyFilled(),
	})

	o.recordTrade(price, qty, taker.OpenOrder.Side)
}

// feeOf returns amount × bps / 10000, floored. It uses a 128-bit intermediate so a
// large amount (quote notional can be huge) cannot overflow. bps is capped at 10000 by
// the DB CHECK, but that constraint lives outside this process (a raw DB edit or a bad
// migration bypasses it) and bits.Div64 panics if the quotient overflows 64 bits — which
// happens whenever bps >= 10000. Clamping here means a misconfigured market charges (at
// most) a 100% fee instead of crashing every market's matcher goroutine.
func feeOf(amount, bps uint64) uint64 {
	if bps == 0 {
		return 0
	}
	if bps > 10000 {
		bps = 10000
	}
	hi, lo := bits.Mul64(amount, bps)
	fee, _ := bits.Div64(hi, lo, 10000)
	return fee
}

func (o *OrderBook) emitMakerFilled(maker *Order, result *repository.BatchResult) {
	result.ClosedOpenOrders = append(result.ClosedOpenOrders, maker.OpenOrder.OrderID)
	result.StatusUpdates = append(result.StatusUpdates, repository.OrderStatusUpdate{
		OrderID: maker.OpenOrder.OrderID,
		Status:  repository.OrderStatusFilled,
	})
	o.recordOrderUpdate(maker.OpenOrder.UserID, maker.OpenOrder.OrderID,
		repository.OrderStatusFilled, maker.OpenOrder.Quantity, 0)
}

func (o *OrderBook) emitMakerPartialFill(maker *Order, result *repository.BatchResult) {
	have, want := restingRemaining(maker, o.market.BaseScale)
	result.OpenOrderUpdates = append(result.OpenOrderUpdates, repository.OpenOrderRemainingUpdate{
		OrderID:             maker.OpenOrder.OrderID,
		RemainingHaveAmount: have,
		RemainingWantAmount: want,
	})
	o.recordOrderUpdate(maker.OpenOrder.UserID, maker.OpenOrder.OrderID,
		repository.OrderStatusPartiallyFilled, maker.OpenOrder.Quantity-maker.Remaining, maker.Remaining)
}

// settleTakerCompletion releases any reserved funds the taker will not use: the price
// improvement on filled volume (it reserved at its limit but traded cheaper) plus, for
// a non-resting order, the unfilled remainder. A resting order keeps exactly the amount
// backing its remaining quantity blocked.
func (o *OrderBook) settleTakerCompletion(t *Order, rests bool, result *repository.BatchResult) {
	// A sell blocks base and spends it as base; a buy blocks quote and spends it as quote.
	held, keep := t.reserve-t.filledBase, t.Remaining
	if t.OpenOrder.Side == oeq.BuyOrder {
		held, keep = t.reserve-t.spentQuote, quoteAmount(t.OpenOrder.Price, t.Remaining, o.market.BaseScale)
	}
	if !rests {
		keep = 0
	}
	if release := held - keep; release > 0 {
		releaseBlocked(result, t.OpenOrder.UserID, o.haveInstr(t.OpenOrder.Side), release)
	}
}

// emitTakerOutcome writes the taker's orders row with its final status and either rests
// it (GTC limit remainder) or records its cancelled remainder. filled is the completion
// verdict from takerFilled — a filled order records no cancelled remainder even if a dust
// budget was refunded.
func (o *OrderBook) emitTakerOutcome(t *Order, rests, filled bool, result *repository.BatchResult) {
	insert := DeriveInsertParams(t.OpenOrder, o.market)
	status := takerStatus(t, rests, filled)
	insert.Status = status
	result.NewOrders = append(result.NewOrders, insert)

	// A quote-denominated market buy has no meaningful base remainder; report 0.
	var remaining uint64
	if !t.quoteDenom {
		remaining = t.Remaining
	}
	o.recordOrderUpdate(t.OpenOrder.UserID, t.OpenOrder.OrderID, status, t.filledBase, remaining)

	if rests {
		o.rest(t)
		o.markLevel(t.OpenOrder.Side, t.OpenOrder.Price)
		have, want := restingRemaining(t, o.market.BaseScale)
		result.OpenOrders = append(result.OpenOrders, repository.InsertOpenOrderParams{
			OrderID:             t.OpenOrder.OrderID,
			Price:               t.OpenOrder.Price,
			MarketID:            o.market.ID,
			Side:                string(t.OpenOrder.Side),
			RemainingHaveAmount: have,
			RemainingWantAmount: want,
		})
		return
	}

	if !filled {
		have, want := canceledRemaining(t, o.market.BaseScale)
		result.CancelledOrders = append(result.CancelledOrders, repository.InsertCancelledOrderParams{
			OrderID:             t.OpenOrder.OrderID,
			RemainingHaveAmount: have,
			RemainingWantAmount: want,
		})
	}
}

func (o *OrderBook) takerRests(t *Order) bool {
	return t.OpenOrder.Type == oeq.LimitOrder &&
		t.OpenOrder.TimeInForce == oeq.GoodTillCancel &&
		t.Remaining > 0
}
