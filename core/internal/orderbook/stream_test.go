package orderbook

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// findBook returns the book delta for one side/price, or ok=false if none was emitted.
func findBook(deltas []marketdata.Book, side string, price uint64) (marketdata.Book, bool) {
	for _, d := range deltas {
		if d.Side == side && d.Price == price {
			return d, true
		}
	}
	return marketdata.Book{}, false
}

func findOrder(orders []StreamOrderUpdate, id uuid.UUID) (marketdata.OrderUpdate, bool) {
	for _, o := range orders {
		if o.Update.OrderID == id.String() {
			return o.Update, true
		}
	}
	return marketdata.OrderUpdate{}, false
}

// Hydration is not a delta: rebuilding the book from persisted orders must not accumulate any stream
// events (otherwise every rebuild would re-announce the whole book as fresh deltas).
func TestStreamHydrationEmitsNothing(t *testing.T) {
	o := testBook()
	restSell(o, uuid.New(), 100, 10)

	s := o.DrainStream()
	if len(s.Trades) != 0 || len(s.Orders) != 0 || len(s.Book) != 0 {
		t.Fatalf("hydration produced events: trades=%d orders=%d book=%d", len(s.Trades), len(s.Orders), len(s.Book))
	}
}

// A full fill produces: one trade tagged with the taker side, one book delta zeroing the emptied
// maker level, and one order update per party (both filled).
func TestStreamFullFill(t *testing.T) {
	o := testBook()
	seller, buyer := uuid.New(), uuid.New()
	sellID := restSell(o, seller, 100, 10)
	o.DrainStream() // discard the hydration (no-op, but make intent explicit)

	taker := &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: buyer, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 120, Quantity: 10,
	}
	o.MatchOrder(taker, repository.NewBatchResult())
	s := o.DrainStream()

	if len(s.Trades) != 1 {
		t.Fatalf("want 1 trade, got %d", len(s.Trades))
	}
	if tr := s.Trades[0]; tr.Price != 100 || tr.Quantity != 10 || tr.TakerSide != "buy" {
		t.Fatalf("trade = %+v (want price 100, qty 10, taker buy)", tr)
	}

	// The maker's ask level at 100 emptied → delta with quantity 0. No bid level (taker fully filled).
	if d, ok := findBook(s.Book, "sell", 100); !ok || d.Quantity != 0 {
		t.Fatalf("sell@100 delta = %+v ok=%v (want qty 0)", d, ok)
	}
	if _, ok := findBook(s.Book, "buy", 120); ok {
		t.Fatalf("taker filled fully; no resting bid level should be emitted")
	}

	if u, ok := findOrder(s.Orders, sellID); !ok || u.Status != repository.OrderStatusFilled || u.Remaining != 0 {
		t.Fatalf("maker update = %+v ok=%v (want filled, remaining 0)", u, ok)
	}
	if u, ok := findOrder(s.Orders, taker.OrderID); !ok || u.Status != repository.OrderStatusFilled || u.Filled != 10 || u.Remaining != 0 {
		t.Fatalf("taker update = %+v ok=%v (want filled, filled 10, remaining 0)", u, ok)
	}
}

// A partial fill that rests emits a delta on BOTH sides: the consumed maker level and the taker's
// new resting bid level, with the taker reported open with its remaining quantity.
func TestStreamPartialFillRests(t *testing.T) {
	o := testBook()
	seller, buyer := uuid.New(), uuid.New()
	restSell(o, seller, 100, 4)
	o.DrainStream()

	taker := &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: buyer, MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 120, Quantity: 10,
	}
	o.MatchOrder(taker, repository.NewBatchResult())
	s := o.DrainStream()

	if d, ok := findBook(s.Book, "sell", 100); !ok || d.Quantity != 0 {
		t.Fatalf("consumed maker level sell@100 = %+v ok=%v (want qty 0)", d, ok)
	}
	if d, ok := findBook(s.Book, "buy", 120); !ok || d.Quantity != 6 {
		t.Fatalf("resting taker level buy@120 = %+v ok=%v (want qty 6)", d, ok)
	}
	if u, ok := findOrder(s.Orders, taker.OrderID); !ok || u.Status != repository.OrderStatusOpen || u.Filled != 4 || u.Remaining != 6 {
		t.Fatalf("taker update = %+v ok=%v (want open, filled 4, remaining 6)", u, ok)
	}
}

// Cancelling a resting order emits a book delta for the freed level and a private cancel update.
func TestStreamCancelEmitsDelta(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	sellID := restSell(o, seller, 100, 7)
	o.DrainStream()

	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: sellID}, repository.NewBatchResult())
	s := o.DrainStream()

	if d, ok := findBook(s.Book, "sell", 100); !ok || d.Quantity != 0 {
		t.Fatalf("cancelled level sell@100 = %+v ok=%v (want qty 0)", d, ok)
	}
	if u, ok := findOrder(s.Orders, sellID); !ok || u.Status != repository.OrderStatusCancelled {
		t.Fatalf("cancel update = %+v ok=%v (want cancelled)", u, ok)
	}
}
