package orderbook

import (
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
)

// statusRejected is the order-update status for an order that could not reserve funds. It is
// persisted as "cancelled" (there is no DB status for it), but the live stream reports it distinctly
// so a bot can tell a rejection apart from a user cancel.
const statusRejected = "rejected"

// levelKey identifies one price level on one side, used to dedupe the set of levels whose aggregate
// quantity changed during a batch.
type levelKey struct {
	side  oeq.OrderSide
	price uint64
}

// orderEvent pairs a private order-lifecycle update with the user it belongs to. The user id is the
// routing key for the private stream (user.<uid>.order) but is deliberately NOT part of the wire
// payload (the broker routes by it; the client already knows it is theirs).
type orderEvent struct {
	userID uuid.UUID
	update marketdata.OrderUpdate
}

// streamEvents accumulates the live market-data events produced while matching one batch. It is
// owned by the OrderBook (single writer, the matcher goroutine) and drained once the batch commits.
// Trades and order updates are recorded directly; book deltas are tracked as the SET of touched
// levels and resolved to their final aggregate quantity at drain time — so a level touched many
// times in one batch emits a single net delta.
type streamEvents struct {
	trades  []marketdata.Trade
	orders  []orderEvent
	touched map[levelKey]struct{}
}

func newStreamEvents() *streamEvents {
	return &streamEvents{touched: make(map[levelKey]struct{})}
}

// StreamSnapshot is one batch's worth of derived live events, ready for the matcher to sequence and
// publish. Book holds the net per-level deltas (Quantity == 0 means the level emptied).
type StreamSnapshot struct {
	Trades []marketdata.Trade
	Orders []StreamOrderUpdate
	Book   []marketdata.Book
}

// StreamOrderUpdate is a private order update plus its owning user id (for routing).
type StreamOrderUpdate struct {
	UserID uuid.UUID
	Update marketdata.OrderUpdate
}

// markLevel flags a price level as changed in this batch. The final quantity is read at drain time.
func (o *OrderBook) markLevel(side oeq.OrderSide, price uint64) {
	o.stream.touched[levelKey{side: side, price: price}] = struct{}{}
}

func (o *OrderBook) recordTrade(price, qty uint64, takerSide oeq.OrderSide) {
	o.stream.trades = append(o.stream.trades, marketdata.Trade{
		Price:     price,
		Quantity:  qty,
		TakerSide: string(takerSide),
	})
}

func (o *OrderBook) recordOrderUpdate(userID, orderID uuid.UUID, status string, filled, remaining uint64) {
	o.stream.orders = append(o.stream.orders, orderEvent{
		userID: userID,
		update: marketdata.OrderUpdate{
			OrderID:   orderID.String(),
			Status:    status,
			Filled:    filled,
			Remaining: remaining,
		},
	})
}

// RecordRejection emits a private "rejected" order update for an order that failed fund reservation.
// Called by the matcher for unfunded orders (which never reach MatchOrder), so the owner still gets
// a lifecycle event. Funds untouched, nothing rested: filled and remaining are zero.
func (o *OrderBook) RecordRejection(userID, orderID uuid.UUID) {
	o.recordOrderUpdate(userID, orderID, statusRejected, 0, 0)
}

// DrainStream returns the events accumulated since the last drain and resets the accumulator. The
// book is read here (post-commit, still on the matcher goroutine) to resolve each touched level to
// its current aggregate quantity. Must be called only after the batch has committed.
func (o *OrderBook) DrainStream() StreamSnapshot {
	out := StreamSnapshot{Trades: o.stream.trades}

	for _, e := range o.stream.orders {
		out.Orders = append(out.Orders, StreamOrderUpdate{UserID: e.userID, Update: e.update})
	}
	for k := range o.stream.touched {
		out.Book = append(out.Book, marketdata.Book{
			Side:     string(k.side),
			Price:    k.price,
			Quantity: o.levelQty(k.side, k.price),
		})
	}

	o.stream = newStreamEvents()
	return out
}

// levelQty returns the aggregate resting quantity at one price level, or 0 if the level no longer
// exists (it emptied and was removed during the batch).
func (o *OrderBook) levelQty(side oeq.OrderSide, price uint64) uint64 {
	if lvl, ok := o.sideTree(side).Get(&PriceLevel{Price: price}); ok {
		return lvl.TotalQty
	}
	return 0
}

// SnapshotLevels returns the full L2 book at the current sequence point, read directly off the book
// on the matcher goroutine (the owner, so no lock). Bids are ordered high→low, asks low→high.
func (o *OrderBook) SnapshotLevels() (bids, asks []marketdata.BookLevel) {
	o.bids.Descend(func(lvl *PriceLevel) bool {
		bids = append(bids, marketdata.BookLevel{Price: lvl.Price, Quantity: lvl.TotalQty})
		return true
	})
	o.asks.Ascend(func(lvl *PriceLevel) bool {
		asks = append(asks, marketdata.BookLevel{Price: lvl.Price, Quantity: lvl.TotalQty})
		return true
	})
	return bids, asks
}

func oppositeSide(s oeq.OrderSide) oeq.OrderSide {
	if s == oeq.BuyOrder {
		return oeq.SellOrder
	}
	return oeq.BuyOrder
}
