package orderbook

import (
	"container/list"
	"math/bits"
	"strings"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/btree"
	"github.com/google/uuid"
)

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

type PriceLevel struct {
	Price uint64
	// First element is the oldest one
	Orders *list.List
	// Total remaining (For FOK orders)
	TotalQty uint64
}

type orderLocator struct {
	el    *list.Element
	level *PriceLevel
	side  oeq.OrderSide
}

// OrderBook is not thread-safe. It must be driven by a single goroutine. One book
// exists per market and accumulates all of a batch's persistent side-effects into the
// *repository.BatchResult passed into MatchOrder / CancelOrder; it performs no I/O.
type OrderBook struct {
	logger *logger.Logger
	market *repository.Market
	bids   *btree.BTreeG[*PriceLevel]
	asks   *btree.BTreeG[*PriceLevel]
	index  map[uuid.UUID]orderLocator
	// stream accumulates the live market-data events of the current batch (see stream.go). It is
	// drained by the matcher after the batch commits. A rebuilt book starts with an empty stream,
	// so events of a failed (rolled-back) batch are never emitted.
	stream *streamEvents
}

func (o *OrderBook) baseInstr() int  { return o.market.BaseInstrumentID }
func (o *OrderBook) quoteInstr() int { return o.market.QuoteInstrumentID }

// BookStats is a read-only snapshot of book depth.
type BookStats struct {
	BidOrders, AskOrders int
	BestBid, BestAsk     uint64
	HasBid, HasAsk       bool
}

// Stats returns the current resting-order counts and best prices per side. It is a pure read
// (no mutation, no I/O): bids are walked high→low and asks low→high, so the first level seen on
// each side is the best. Cost is O(price levels) — list lengths are O(1) — not O(orders).
func (o *OrderBook) Stats() BookStats {
	var s BookStats
	o.bids.Descend(func(lvl *PriceLevel) bool {
		if !s.HasBid {
			s.BestBid, s.HasBid = lvl.Price, true
		}
		s.BidOrders += lvl.Orders.Len()
		return true
	})
	o.asks.Ascend(func(lvl *PriceLevel) bool {
		if !s.HasAsk {
			s.BestAsk, s.HasAsk = lvl.Price, true
		}
		s.AskOrders += lvl.Orders.Len()
		return true
	})
	return s
}

// MatchOrder runs one taker order against the book, accumulating every fill, status
// transition, resting/cancellation record and balance movement into result. The
// order's funds have already been reserved (balance -> blocked) by the caller before
// this is invoked.
func (o *OrderBook) MatchOrder(event *oeq.OpenOrderEvent, result *repository.BatchResult) {
	taker := newOrder(event, o.market.BaseScale)

	switch {
	case !guardsOK(taker):
		// Defensive: ValidateOrderEvent should already reject these. Skip matching;
		// completion below releases the reservation and records a cancellation.
		o.logger.Warn("orderbook: order failed defensive guards, rejecting")
	case taker.OpenOrder.TimeInForce == oeq.FillOrKill && !o.canFill(taker):
		// FOK that cannot fully fill is killed untouched — skip matching.
	default:
		o.match(taker, result)
	}

	rests := o.takerRests(taker)
	o.settleTakerCompletion(taker, rests, result)
	o.emitTakerOutcome(taker, rests, result)
}

func (o *OrderBook) match(taker *Order, result *repository.BatchResult) {
	var emptyLevels []*PriceLevel

	o.eachOppositeLevel(taker, func(lvl *PriceLevel) bool {
		if !crosses(taker, lvl.Price) {
			return false
		}

		for lvl.Orders.Len() > 0 && taker.canTrade(lvl.Price) {
			front := lvl.Orders.Front()
			maker, ok := front.Value.(*Order)
			if !ok {
				o.logger.Error("orderbook: corrupt list element in match")
				lvl.Orders.Remove(front)
				continue
			}

			qty := fillQty(taker, maker, lvl.Price)
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

	// Delete empty levels after iteration — modifying the tree inside Ascend/Descend is unsafe.
	oppTree := o.oppositeTree(taker.OpenOrder.Side)
	for _, lvl := range emptyLevels {
		oppTree.Delete(lvl)
	}
}

// canFill reports whether the crossing liquidity can fully satisfy a FOK taker.
func (o *OrderBook) canFill(taker *Order) bool {
	if taker.quoteDenom {
		// Market buy FOK: is there enough ask value to spend the whole budget?
		var value uint64
		budget := taker.RemainingQuote
		o.eachOppositeLevel(taker, func(lvl *PriceLevel) bool {
			if !crosses(taker, lvl.Price) {
				return false
			}
			remaining := budget - value
			// Ceiling division avoids uint64 overflow in lvl.Price*lvl.TotalQty when the
			// per-level notional is large. If this level's quantity alone covers the rest
			// of the budget, saturate and stop — no need to keep accumulating.
			if lvl.Price > 0 && lvl.TotalQty >= (remaining+lvl.Price-1)/lvl.Price {
				value = budget
				return false
			}
			value += lvl.Price * lvl.TotalQty
			return value < budget
		})
		return value >= taker.RemainingQuote
	}

	var avail uint64
	need := taker.Remaining
	o.eachOppositeLevel(taker, func(lvl *PriceLevel) bool {
		if !crosses(taker, lvl.Price) {
			return false
		}
		avail += lvl.TotalQty
		return avail < need
	})
	return avail >= need
}

// CancelOrder removes a resting order, releases its remaining reservation and records
// the cancellation. A miss is a normal logical race (the order may have filled, never
// existed, or already been cancelled) and is an idempotent no-op.
func (o *OrderBook) CancelOrder(event *oeq.CancelOrderEvent, result *repository.BatchResult) {
	loc, ok := o.index[event.OrderID]
	if !ok {
		// @TODO(P-events): emit cancel-reject event once Queue 2 exists.
		return
	}

	stored, ok2 := loc.el.Value.(*Order)
	if !ok2 {
		o.logger.Error("orderbook: corrupt list element in CancelOrder")
		return
	}

	loc.level.Orders.Remove(loc.el)
	loc.level.TotalQty -= stored.Remaining
	o.markLevel(loc.side, loc.level.Price)
	delete(o.index, event.OrderID)
	if loc.level.Orders.Len() == 0 {
		o.sideTree(loc.side).Delete(loc.level)
	}

	have, want := restingRemaining(stored, o.market.BaseScale)

	if stored.OpenOrder.Side == oeq.BuyOrder {
		result.AddBalanceDelta(stored.OpenOrder.UserID, o.quoteInstr(), int64(have), -int64(have))
	} else {
		result.AddBalanceDelta(stored.OpenOrder.UserID, o.baseInstr(), int64(have), -int64(have))
	}

	result.ClosedOpenOrders = append(result.ClosedOpenOrders, event.OrderID)
	result.CancelledOrders = append(result.CancelledOrders, repository.InsertCancelledOrderParams{
		OrderID:             event.OrderID,
		RemainingHaveAmount: have,
		RemainingWantAmount: want,
	})

	// A partially filled order keeps the "partially_filled" terminal status; one that
	// never traded becomes "cancelled". (Original qty is lost after hydration, so a
	// rebuilt order always reports "cancelled" — see Hydrate.)
	status := repository.OrderStatusCancelled
	if stored.Remaining < stored.OpenOrder.Quantity {
		status = repository.OrderStatusPartiallyFilled
	}
	result.StatusUpdates = append(result.StatusUpdates, repository.OrderStatusUpdate{
		OrderID: event.OrderID,
		Status:  status,
	})
	o.recordOrderUpdate(stored.OpenOrder.UserID, event.OrderID, status,
		stored.OpenOrder.Quantity-stored.Remaining, stored.Remaining)
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

	// Fees are charged on the asset each party receives, at the taker or maker rate
	// per role. They simply leave the traders' balances (no house account) and are
	// recorded on the match. The buyer receives base, the seller receives quote.
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
		MatchBuyFees:      buyerFee,  // buyer's fee, in base
		MatchSellFees:     sellerFee, // seller's fee, in quote
		BuyOrderIsTaker:   buyerIsTaker,
		IsBuyOrderFilled:  buyer.fullyFilled(),
		IsSellOrderFilled: seller.fullyFilled(),
	})

	o.recordTrade(price, qty, taker.OpenOrder.Side)
}

// feeOf returns amount × bps / 10000, floored. It uses a 128-bit intermediate so a
// large amount (quote notional can be huge) cannot overflow; bps is capped at 10000
// by the DB CHECK, but the guard keeps a misconfiguration from panicking Div64.
func feeOf(amount, bps uint64) uint64 {
	if bps == 0 {
		return 0
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
	if t.OpenOrder.Side == oeq.BuyOrder {
		held := t.reserve - t.spentQuote // quote still blocked after fills
		var keep uint64
		if rests {
			keep = quoteAmount(t.OpenOrder.Price, t.Remaining, o.market.BaseScale)
		}
		if release := held - keep; release > 0 {
			result.AddBalanceDelta(t.OpenOrder.UserID, o.quoteInstr(), int64(release), -int64(release))
		}
		return
	}

	held := t.reserve - t.filledBase // base still blocked after fills
	var keep uint64
	if rests {
		keep = t.Remaining
	}
	if release := held - keep; release > 0 {
		result.AddBalanceDelta(t.OpenOrder.UserID, o.baseInstr(), int64(release), -int64(release))
	}
}

// emitTakerOutcome writes the taker's orders row with its final status and either rests
// it (GTC limit remainder) or records its cancelled remainder.
func (o *OrderBook) emitTakerOutcome(t *Order, rests bool, result *repository.BatchResult) {
	insert := DeriveInsertParams(t.OpenOrder, o.market)
	status := takerStatus(t, rests)
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

	if !t.fullyFilled() {
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

func (o *OrderBook) rest(order *Order) {
	lvl := o.getOrCreate(order.OpenOrder.Side, order.OpenOrder.Price)
	el := lvl.Orders.PushBack(order)
	lvl.TotalQty += order.Remaining
	o.index[order.OpenOrder.OrderID] = orderLocator{
		el:    el,
		level: lvl,
		side:  order.OpenOrder.Side,
	}
}

func (o *OrderBook) getOrCreate(side oeq.OrderSide, price uint64) *PriceLevel {
	tree := o.sideTree(side)
	key := &PriceLevel{Price: price}
	if lvl, ok := tree.Get(key); ok {
		return lvl
	}
	lvl := &PriceLevel{
		Price:  price,
		Orders: list.New(),
	}
	tree.ReplaceOrInsert(lvl)
	return lvl
}

func (o *OrderBook) oppositeTree(side oeq.OrderSide) *btree.BTreeG[*PriceLevel] {
	if side == oeq.BuyOrder {
		return o.asks
	}
	return o.bids
}

func (o *OrderBook) sideTree(side oeq.OrderSide) *btree.BTreeG[*PriceLevel] {
	if side == oeq.BuyOrder {
		return o.bids
	}
	return o.asks
}

func (o *OrderBook) eachOppositeLevel(order *Order, fn func(*PriceLevel) bool) {
	if order.OpenOrder.Side == oeq.BuyOrder {
		o.asks.Ascend(fn)
	} else {
		o.bids.Descend(fn)
	}
}

// Hydrate rebuilds the resting book from persisted open orders. Rows must be supplied
// in ascending open_orders.id order so per-price-level FIFO priority is preserved.
// The pre-restart original quantity is not persisted, so a hydrated order's Quantity
// is set to its remaining base amount (see CancelOrder for the consequence).
func (o *OrderBook) Hydrate(rows []repository.OpenOrderHydration) {
	for _, r := range rows {
		base := hydrateBase(r)
		event := &oeq.OpenOrderEvent{
			OrderID:     r.OrderID,
			UserID:      r.UserID,
			MarketID:    o.market.ID,
			Side:        oeq.OrderSide(r.Side),
			Type:        oeq.OrderType(r.Type),
			TimeInForce: tifFromDB(r.TimeInForce),
			Price:       r.Price,
			Quantity:    base,
			ExpiresAt:   r.ExpiresAt,
		}
		if r.ClientOrderID != nil {
			event.ClientOrderID = *r.ClientOrderID
		}
		o.rest(&Order{OpenOrder: event, Remaining: base})
	}
}

// --- pure helpers ---

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

func hydrateBase(r repository.OpenOrderHydration) uint64 {
	if oeq.OrderSide(r.Side) == oeq.BuyOrder {
		return r.RemainingWantAmount // want = base for a buy
	}
	return r.RemainingHaveAmount // have = base for a sell
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

// DeriveInsertParams maps an order event + market to the repository insert params.
// Status is left empty for the caller to set from the matching outcome.
//
// Limit buy:  have=quote, want=base; have_qty = price×qty, want_qty = qty
// Limit sell: have=base,  want=quote; have_qty = qty,       want_qty = price×qty
// Market buy: quote-denominated; have_qty = quote_qty (want unknown until executed)
// Market sell: base-denominated;  have_qty = qty       (want unknown until executed)
func DeriveInsertParams(event *oeq.OpenOrderEvent, market *repository.Market) repository.InsertOrderParams {
	p := repository.InsertOrderParams{
		ID:          event.OrderID,
		UserID:      event.UserID,
		Type:        string(event.Type), // 'limit'/'market' already match the DB enum
		TimeInForce: tifToDB(event.TimeInForce),
	}

	if event.ClientOrderID != "" {
		p.ClientOrderID = &event.ClientOrderID
	}
	if event.ExpiresAt != nil {
		t := time.Unix(*event.ExpiresAt, 0)
		p.ExpiresAt = &t
	}

	if event.Side == oeq.BuyOrder {
		p.HaveInstrumentID = market.QuoteInstrumentID
		p.WantInstrumentID = market.BaseInstrumentID
	} else {
		p.HaveInstrumentID = market.BaseInstrumentID
		p.WantInstrumentID = market.QuoteInstrumentID
	}

	switch event.Type {
	case oeq.LimitOrder:
		notional := quoteAmount(event.Price, event.Quantity, market.BaseScale)
		qty := event.Quantity
		if event.Side == oeq.BuyOrder {
			p.HaveQuantity = &notional
			p.WantQuantity = &qty
		} else {
			p.HaveQuantity = &qty
			p.WantQuantity = &notional
		}
	case oeq.MarketOrder:
		if event.Side == oeq.BuyOrder {
			p.HaveQuantity = event.QuoteQty // quote budget
		} else {
			qty := event.Quantity
			p.HaveQuantity = &qty // base offered
		}
	}

	return p
}

// tifToDB / tifFromDB bridge the lowercase engine enum and the uppercase DB CHECK.
func tifToDB(t oeq.TimeInForce) string {
	switch t {
	case oeq.GoodTillCancel:
		return "GTC"
	case oeq.ImmediateOrCancel:
		return "IOC"
	case oeq.FillOrKill:
		return "FOK"
	default:
		return strings.ToUpper(string(t))
	}
}

func tifFromDB(s string) oeq.TimeInForce {
	switch s {
	case "GTC":
		return oeq.GoodTillCancel
	case "IOC":
		return oeq.ImmediateOrCancel
	case "FOK":
		return oeq.FillOrKill
	default:
		return oeq.TimeInForce(strings.ToLower(s))
	}
}

const btreeDegree = 32

func priceLess(a, b *PriceLevel) bool { return a.Price < b.Price }

func NewOrderBook(
	log *logger.Logger,
	market *repository.Market,
) *OrderBook {
	if log == nil {
		panic("logger cannot be nil")
	}
	if market == nil {
		panic("market cannot be nil")
	}

	bids := btree.NewG(btreeDegree, priceLess)
	asks := btree.NewG(btreeDegree, priceLess)
	return &OrderBook{
		logger: log,
		market: market,
		bids:   bids,
		asks:   asks,
		index:  make(map[uuid.UUID]orderLocator),
		stream: newStreamEvents(),
	}
}
