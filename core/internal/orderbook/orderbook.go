package orderbook

import (
	"container/list"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/btree"
	"github.com/google/uuid"
)

// This file holds the book's core structure — the OrderBook type, its price-level trees,
// and the order index — plus the primitives every other file in this package builds on
// (rest, the tree/side lookups). Matching lives in match.go, cancel/expiry in cancel.go and
// expiry.go, hydration and DB-param mapping in hydrate.go, live-stream events in stream.go.

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
	// expiries indexes resting orders with a TTL, ordered by expiry time, so ExpireDue
	// (expiry.go) finds due orders in O(k log n) instead of scanning the whole book.
	expiries *btree.BTreeG[*expiryEntry]
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

// Stats is a pure read (no mutation, no I/O): bids are walked high→low, asks low→high, so
// the first level seen is the best. Cost is O(price levels), not O(orders).
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

func (o *OrderBook) rest(order *Order) {
	lvl := o.getOrCreate(order.OpenOrder.Side, order.OpenOrder.Price)
	el := lvl.Orders.PushBack(order)
	lvl.TotalQty += order.Remaining
	o.index[order.OpenOrder.OrderID] = orderLocator{
		el:    el,
		level: lvl,
		side:  order.OpenOrder.Side,
	}
	o.indexExpiry(order)
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
	expiries := btree.NewG(btreeDegree, expiryLess)
	return &OrderBook{
		logger:   log,
		market:   market,
		bids:     bids,
		asks:     asks,
		expiries: expiries,
		index:    make(map[uuid.UUID]orderLocator),
		stream:   newStreamEvents(),
	}
}
