package orderbook

import (
	"math"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// maxTriggerSweep is an effectively-unlimited cap, for tests asserting *which* exits are due.
const maxTriggerSweep = math.MaxInt

func u64(v uint64) *uint64 { return &v }

// bracketBuy is a limit GTC buy at price for qty base, taking profit at tp and stopping at sl
// (either may be 0 for "unset"). Its exit is a sell of what it receives.
func bracketBuy(user uuid.UUID, price, qty, tp, sl uint64) *oeq.OpenOrderEvent {
	e := &oeq.OpenOrderEvent{
		OrderID:     uuid.New(),
		UserID:      user,
		MarketID:    1,
		Side:        oeq.BuyOrder,
		Type:        oeq.LimitOrder,
		TimeInForce: oeq.GoodTillCancel,
		Price:       price,
		Quantity:    qty,
	}
	if tp != 0 {
		e.TakeProfitPrice = u64(tp)
	}
	if sl != 0 {
		e.StopLossPrice = u64(sl)
	}
	return e
}

// fillBracketBuy rests a sell and takes it with a bracket buy, returning the armed exit's id.
func fillBracketBuy(t *testing.T, o *OrderBook, buyer uuid.UUID, price, qty, tp, sl uint64) (entry, exit uuid.UUID) {
	t.Helper()
	restSell(o, uuid.New(), price, qty)
	e := bracketBuy(buyer, price, qty, tp, sl)
	r := repository.NewBatchResult()
	o.MatchOrder(e, r)
	if len(r.Matches) != 1 || r.NewOrders[0].Status != repository.OrderStatusFilled {
		t.Fatalf("entry did not fill: matches=%d orders=%+v", len(r.Matches), r.NewOrders)
	}
	return e.OrderID, oeq.ExitOrderID(e.OrderID)
}

// tradeAt moves the market's last price by matching two strangers at price.
func tradeAt(t *testing.T, o *OrderBook, price uint64) {
	t.Helper()
	restSell(o, uuid.New(), price, 1)
	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.ImmediateOrCancel,
		Price: price, Quantity: 1,
	}, r)
	if len(r.Matches) != 1 || r.Matches[0].MatchPrice != price {
		t.Fatalf("could not print a trade at %d: %+v", price, r.Matches)
	}
}

func findInsert(r *repository.BatchResult, id uuid.UUID) *repository.InsertOrderParams {
	for i := range r.NewOrders {
		if r.NewOrders[i].ID == id {
			return &r.NewOrders[i]
		}
	}
	return nil
}

func findStatusUpdate(r *repository.BatchResult, id uuid.UUID) *repository.OrderStatusUpdate {
	for i := range r.StatusUpdates {
		if r.StatusUpdates[i].OrderID == id {
			return &r.StatusUpdates[i]
		}
	}
	return nil
}

// The whole feature rests on this: a bracket entry's proceeds are blocked in the same result as
// the fill that produced them, and the exit that will trade them is persisted pending, sized to
// exactly what was received, linked to its entry and carrying its triggers.
func TestBracketBuyFillBlocksProceedsAndArmsSellExit(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	restSell(o, uuid.New(), 100, 10)

	entry := bracketBuy(buyer, 100, 10, 150, 50)
	r := repository.NewBatchResult()
	o.MatchOrder(entry, r)

	bb := delta(t, r, buyer, baseInstr)
	if bb.BalanceDelta != 0 || bb.BlockedDelta != 10 {
		t.Fatalf("buyer base: balance=%d blocked=%d (want 0, 10 — proceeds go straight to blocked)", bb.BalanceDelta, bb.BlockedDelta)
	}
	assertConserved(t, r)

	exitID := oeq.ExitOrderID(entry.OrderID)
	exit := findInsert(r, exitID)
	if exit == nil {
		t.Fatalf("no orders row for the exit in %+v", r.NewOrders)
	}
	if exit.Status != repository.OrderStatusPending || exit.Type != "market" || exit.TimeInForce != "IOC" {
		t.Fatalf("exit row = %+v, want a pending market IOC", exit)
	}
	if exit.HaveInstrumentID != baseInstr || exit.HaveQuantity == nil || *exit.HaveQuantity != 10 {
		t.Fatalf("exit have = (%d, %v), want 10 base", exit.HaveInstrumentID, exit.HaveQuantity)
	}
	if exit.ParentOrderID == nil || *exit.ParentOrderID != entry.OrderID {
		t.Fatalf("exit parent = %v, want %s", exit.ParentOrderID, entry.OrderID)
	}
	if *exit.TakeProfitPrice != 150 || *exit.StopLossPrice != 50 {
		t.Fatalf("exit triggers = (%d, %d), want (150, 50)", *exit.TakeProfitPrice, *exit.StopLossPrice)
	}

	if _, ok := o.pending[exitID]; !ok {
		t.Fatal("exit not indexed as pending")
	}
	if o.HasTriggered() {
		t.Fatal("exit reported due at the entry's own fill price")
	}
	if upd := findOrderUpdate(o.DrainStream(), exitID); upd == nil || upd.Status != repository.OrderStatusPending || upd.Remaining != 10 {
		t.Fatalf("stream update for the exit = %+v, want pending with remaining 10", upd)
	}
}

// A sell entry receives quote, so its exit is a quote-denominated market buy — the same
// denomination rule every market order follows.
func TestBracketSellFillArmsQuoteDenominatedBuyExit(t *testing.T) {
	o := testBook()
	seller := uuid.New()
	restBuy(o, uuid.New(), 100, 10)

	entry := &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: seller, MarketID: 1,
		Side: oeq.SellOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10, TakeProfitPrice: u64(50), StopLossPrice: u64(150),
	}
	r := repository.NewBatchResult()
	o.MatchOrder(entry, r)

	sq := delta(t, r, seller, quoteInstr)
	if sq.BalanceDelta != 0 || sq.BlockedDelta != 1000 {
		t.Fatalf("seller quote: balance=%d blocked=%d (want 0, 1000)", sq.BalanceDelta, sq.BlockedDelta)
	}
	exit := findInsert(r, oeq.ExitOrderID(entry.OrderID))
	if exit == nil || exit.HaveInstrumentID != quoteInstr || *exit.HaveQuantity != 1000 || exit.WantQuantity != nil {
		t.Fatalf("exit row = %+v, want have 1000 quote and no want", exit)
	}
	p := o.pending[exit.ID]
	if p.event.Side != oeq.BuyOrder || p.event.QuoteQty == nil || *p.event.QuoteQty != 1000 {
		t.Fatalf("pending exit event = %+v, want a buy with quote_qty 1000", p.event)
	}
	// Buy exit: take profit fires on the way down, stop loss on the way up.
	if p.below == nil || p.below.price != 50 || p.above == nil || p.above.price != 150 {
		t.Fatalf("triggers indexed as above=%+v below=%+v, want above 150 / below 50", p.above, p.below)
	}
}

// The exit is sized by what the entry actually received, which is net of the taker fee.
func TestBracketProceedsAreNetOfFees(t *testing.T) {
	o := NewOrderBook(logger.NewLogger(logger.Error), &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: 1,
		TakerFeeBps: 100, // 1%
	})
	buyer := uuid.New()
	restSell(o, uuid.New(), 100, 100)

	entry := bracketBuy(buyer, 100, 100, 150, 0)
	r := repository.NewBatchResult()
	o.MatchOrder(entry, r)

	bb := delta(t, r, buyer, baseInstr)
	if bb.BlockedDelta != 99 || bb.BalanceDelta != 0 {
		t.Fatalf("buyer base: blocked=%d balance=%d (want 99, 0)", bb.BlockedDelta, bb.BalanceDelta)
	}
	if exit := findInsert(r, oeq.ExitOrderID(entry.OrderID)); exit == nil || *exit.HaveQuantity != 99 {
		t.Fatalf("exit sized %v, want 99 (100 bought minus 1 fee)", exit)
	}
	assertConserved(t, r)
}

// A take profit on a sell exit fires once the market trades at or above it. Firing sends the
// exit through the ordinary taker path — it trades, its blocked base leaves, quote arrives in
// balance — but its row is updated, not inserted again, and it arms nothing of its own.
func TestTakeProfitFiresOnTheWayUp(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	_, exitID := fillBracketBuy(t, o, buyer, 100, 10, 150, 50)

	if o.HasTriggered() {
		t.Fatal("due before any price move")
	}
	tradeAt(t, o, 149)
	if o.HasTriggered() || len(o.TriggeredExits(maxTriggerSweep)) != 0 {
		t.Fatal("due one tick below the take profit")
	}
	tradeAt(t, o, 150)
	if !o.HasTriggered() {
		t.Fatal("not due at the take profit")
	}
	if due := o.TriggeredExits(maxTriggerSweep); len(due) != 1 || due[0] != exitID {
		t.Fatalf("TriggeredExits = %v, want [%s]", due, exitID)
	}

	bidder := uuid.New()
	restBuy(o, bidder, 140, 10)
	r := repository.NewBatchResult()
	o.FireExit(exitID, r)

	if len(r.Matches) != 1 || r.Matches[0].MatchPrice != 140 || r.Matches[0].SellOrderID != exitID {
		t.Fatalf("exit fill = %+v, want 10 sold at 140", r.Matches)
	}
	if findInsert(r, exitID) != nil {
		t.Fatal("fired exit was inserted again — its row already exists")
	}
	if upd := findStatusUpdate(r, exitID); upd == nil || upd.Status != repository.OrderStatusFilled {
		t.Fatalf("exit status update = %+v, want filled", upd)
	}
	bb := delta(t, r, buyer, baseInstr)
	if bb.BlockedDelta != -10 || bb.BalanceDelta != 0 {
		t.Fatalf("owner base: blocked=%d balance=%d (want -10, 0)", bb.BlockedDelta, bb.BalanceDelta)
	}
	if bq := delta(t, r, buyer, quoteInstr); bq.BalanceDelta != 1400 || bq.BlockedDelta != 0 {
		t.Fatalf("owner quote: balance=%d blocked=%d (want 1400, 0)", bq.BalanceDelta, bq.BlockedDelta)
	}
	assertConserved(t, r)

	if len(o.pending) != 0 || o.fireAbove.Len() != 0 || o.fireBelow.Len() != 0 {
		t.Fatalf("index not cleared: pending=%d above=%d below=%d — the sibling stop loss must go with it", len(o.pending), o.fireAbove.Len(), o.fireBelow.Len())
	}
	if upd := findOrderUpdate(o.DrainStream(), exitID); upd == nil || upd.Status != repository.OrderStatusFilled {
		t.Fatalf("stream update for the fired exit = %+v, want filled", upd)
	}
}

// The mirror: a stop loss on a sell exit fires once the market trades at or below it.
func TestStopLossFiresOnTheWayDown(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	_, exitID := fillBracketBuy(t, o, buyer, 100, 10, 150, 50)

	tradeAt(t, o, 51)
	if o.HasTriggered() {
		t.Fatal("due one tick above the stop loss")
	}
	tradeAt(t, o, 50)
	if due := o.TriggeredExits(maxTriggerSweep); len(due) != 1 || due[0] != exitID {
		t.Fatalf("TriggeredExits = %v, want [%s]", due, exitID)
	}

	restBuy(o, uuid.New(), 45, 10)
	r := repository.NewBatchResult()
	o.FireExit(exitID, r)
	if len(r.Matches) != 1 || r.Matches[0].MatchPrice != 45 {
		t.Fatalf("exit fill = %+v, want 10 sold at 45", r.Matches)
	}
	if len(o.pending) != 0 || o.fireAbove.Len() != 0 {
		t.Fatal("sibling take profit survived the stop loss firing")
	}
	assertConserved(t, r)
}

// A market IOC into an empty book fills nothing: the exit is cancelled and its blocked amount
// goes back to balance. The position is simply held, not lost.
func TestExitFiringIntoEmptyBookIsCancelledAndReleased(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	_, exitID := fillBracketBuy(t, o, buyer, 100, 10, 150, 0)
	tradeAt(t, o, 150)

	r := repository.NewBatchResult()
	o.FireExit(exitID, r)

	if len(r.Matches) != 0 {
		t.Fatalf("traded against nothing: %+v", r.Matches)
	}
	if upd := findStatusUpdate(r, exitID); upd == nil || upd.Status != repository.OrderStatusCancelled {
		t.Fatalf("exit status update = %+v, want cancelled", upd)
	}
	if len(r.CancelledOrders) != 1 || r.CancelledOrders[0].OrderID != exitID || r.CancelledOrders[0].RemainingHaveAmount != 10 {
		t.Fatalf("CancelledOrders = %+v, want the exit with 10 base remaining", r.CancelledOrders)
	}
	if bb := delta(t, r, buyer, baseInstr); bb.BalanceDelta != 10 || bb.BlockedDelta != -10 {
		t.Fatalf("owner base: balance=%d blocked=%d (want 10, -10)", bb.BalanceDelta, bb.BlockedDelta)
	}
	if len(o.pending) != 0 {
		t.Fatal("cancelled exit still pending")
	}
}

// A user may cancel a parked exit like any live order. Its whole blocked amount is released,
// it is recorded cancelled, and neither trigger can fire it afterwards.
func TestCancelPendingExitReleasesBlockedProceeds(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	_, exitID := fillBracketBuy(t, o, buyer, 100, 10, 150, 50)

	if owner, ok := o.RestingOwner(exitID); !ok || owner != buyer {
		t.Fatalf("RestingOwner = (%s, %v), want the buyer", owner, ok)
	}

	r := repository.NewBatchResult()
	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: exitID}, r)

	if bb := delta(t, r, buyer, baseInstr); bb.BalanceDelta != 10 || bb.BlockedDelta != -10 {
		t.Fatalf("owner base: balance=%d blocked=%d (want 10, -10)", bb.BalanceDelta, bb.BlockedDelta)
	}
	if upd := findStatusUpdate(r, exitID); upd == nil || upd.Status != repository.OrderStatusCancelled {
		t.Fatalf("status update = %+v, want cancelled", upd)
	}
	if len(r.CancelledOrders) != 1 || r.CancelledOrders[0].RemainingHaveAmount != 10 {
		t.Fatalf("CancelledOrders = %+v, want 10 base remaining", r.CancelledOrders)
	}
	if len(r.ClosedOpenOrders) != 0 {
		t.Fatal("a pending exit has no open_orders row to delete")
	}
	if len(o.pending) != 0 || o.fireAbove.Len() != 0 || o.fireBelow.Len() != 0 {
		t.Fatal("cancelled exit still indexed")
	}
	if _, ok := o.RestingOwner(exitID); ok {
		t.Fatal("cancelled exit still has an owner")
	}
	tradeAt(t, o, 150)
	if o.HasTriggered() {
		t.Fatal("cancelled exit reported due")
	}
	if upd := findOrderUpdate(o.DrainStream(), exitID); upd == nil || upd.Status != repository.OrderStatusCancelled {
		t.Fatalf("stream update = %+v, want cancelled", upd)
	}

	// Idempotent: a second cancel is a no-op.
	r2 := repository.NewBatchResult()
	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: exitID}, r2)
	if len(r2.StatusUpdates) != 0 || len(r2.BalanceDeltas()) != 0 {
		t.Fatalf("second cancel had effects: %+v", r2)
	}
}

// An entry that rested after a partial fill and was then cancelled still owns the part it
// bought, so the exit is armed for that part when the cancel closes it — not at the fill.
func TestPartialFillThenCancelArmsExitForTheFilledPart(t *testing.T) {
	o := testBook()
	buyer := uuid.New()
	restSell(o, uuid.New(), 100, 4)

	entry := bracketBuy(buyer, 100, 10, 150, 0)
	r := repository.NewBatchResult()
	o.MatchOrder(entry, r)
	if findInsert(r, oeq.ExitOrderID(entry.OrderID)) != nil || len(o.pending) != 0 {
		t.Fatal("exit armed while the entry is still resting")
	}
	if bb := delta(t, r, buyer, baseInstr); bb.BlockedDelta != 4 || bb.BalanceDelta != 0 {
		t.Fatalf("buyer base after partial fill: blocked=%d balance=%d (want 4, 0)", bb.BlockedDelta, bb.BalanceDelta)
	}

	r2 := repository.NewBatchResult()
	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: entry.OrderID}, r2)

	if upd := findStatusUpdate(r2, entry.OrderID); upd == nil || upd.Status != repository.OrderStatusPartiallyFilled {
		t.Fatalf("entry status = %+v, want partially_filled", upd)
	}
	exit := findInsert(r2, oeq.ExitOrderID(entry.OrderID))
	if exit == nil || *exit.HaveQuantity != 4 {
		t.Fatalf("exit = %+v, want one for the 4 base received", exit)
	}
	// The cancel releases only the unfilled 6 × 100 quote; the 4 base stays blocked for the exit.
	if bq := delta(t, r2, buyer, quoteInstr); bq.BalanceDelta != 600 || bq.BlockedDelta != -600 {
		t.Fatalf("buyer quote on cancel: balance=%d blocked=%d (want 600, -600)", bq.BalanceDelta, bq.BlockedDelta)
	}
	if bb := delta(t, r2, buyer, baseInstr); bb.BalanceDelta != 0 || bb.BlockedDelta != 0 {
		t.Fatalf("buyer base on cancel: balance=%d blocked=%d (want untouched)", bb.BalanceDelta, bb.BlockedDelta)
	}
}

// Nothing bought, nothing to exit: an entry cancelled or killed unfilled arms no exit and leaves
// no pending row behind.
func TestUnfilledEntryArmsNothing(t *testing.T) {
	o := testBook()
	buyer := uuid.New()

	resting := bracketBuy(buyer, 100, 10, 150, 50)
	r := repository.NewBatchResult()
	o.MatchOrder(resting, r)
	o.CancelOrder(&oeq.CancelOrderEvent{OrderID: resting.OrderID}, r)

	ioc := bracketBuy(buyer, 100, 10, 150, 50)
	ioc.TimeInForce = oeq.ImmediateOrCancel
	o.MatchOrder(ioc, r)

	if len(r.NewOrders) != 2 || len(o.pending) != 0 {
		t.Fatalf("NewOrders = %+v, pending = %d; want only the two entries and nothing pending", r.NewOrders, len(o.pending))
	}
}

// A resting bracket entry is a maker: its exit arms when the last of it is taken, sized to
// everything it received — including fills from before a restart, which hydration carries as
// Received.
func TestMakerBracketArmsOnFullFillIncludingPreRestartFills(t *testing.T) {
	o := testBook()
	buyer, entryID := uuid.New(), uuid.New()
	o.Hydrate([]repository.OpenOrderHydration{{
		OrderID: entryID, UserID: buyer, Side: "buy", Price: 100, Type: "limit", TimeInForce: "GTC",
		RemainingHaveAmount: 700, RemainingWantAmount: 7, // 7 of 10 still to buy
		TakeProfitPrice: u64(150), Received: 3, // 3 bought before the restart
	}})

	r := repository.NewBatchResult()
	o.MatchOrder(&oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.SellOrder, Type: oeq.LimitOrder, TimeInForce: oeq.ImmediateOrCancel,
		Price: 100, Quantity: 7,
	}, r)

	if bb := delta(t, r, buyer, baseInstr); bb.BlockedDelta != 7 || bb.BalanceDelta != 0 {
		t.Fatalf("maker base: blocked=%d balance=%d (want 7, 0)", bb.BlockedDelta, bb.BalanceDelta)
	}
	exit := findInsert(r, oeq.ExitOrderID(entryID))
	if exit == nil || *exit.HaveQuantity != 10 || *exit.ParentOrderID != entryID {
		t.Fatalf("exit = %+v, want 10 base (3 hydrated + 7 now) parented to the entry", exit)
	}
	if upd := findStatusUpdate(r, entryID); upd == nil || upd.Status != repository.OrderStatusFilled {
		t.Fatalf("entry status = %+v, want filled", upd)
	}
	assertConserved(t, r)
}

// A rebuilt book must know every parked exit and the price they are judged against, or a
// restart would silently disarm every bracket.
func TestHydratePendingRestoresTriggersAndLastPrice(t *testing.T) {
	o := testBook()
	owner, entryID := uuid.New(), uuid.New()
	exitID := oeq.ExitOrderID(entryID)
	o.HydratePending([]repository.PendingOrderHydration{{
		OrderID: exitID, UserID: owner, ParentOrderID: entryID, Side: "sell", Amount: 10,
		TakeProfitPrice: u64(150), StopLossPrice: u64(50),
	}})

	if o.HasTriggered() {
		t.Fatal("due before the market has a last price")
	}
	o.SetLastPrice(150)
	if due := o.TriggeredExits(maxTriggerSweep); len(due) != 1 || due[0] != exitID {
		t.Fatalf("TriggeredExits after hydration = %v, want [%s]", due, exitID)
	}

	restBuy(o, uuid.New(), 140, 10)
	r := repository.NewBatchResult()
	o.FireExit(exitID, r)
	if len(r.Matches) != 1 || r.Matches[0].SellOrderID != exitID || r.Matches[0].MatchBuyAmount != 10 {
		t.Fatalf("hydrated exit fill = %+v, want 10 sold", r.Matches)
	}
	if bb := delta(t, r, owner, baseInstr); bb.BlockedDelta != -10 {
		t.Fatalf("owner base blocked delta = %d, want -10 (hydrated amount was blocked before the restart)", bb.BlockedDelta)
	}
	if len(o.pending) != 0 {
		t.Fatal("hydrated exit not removed after firing")
	}
}

// Firing is bounded per batch like the expiry sweep; the rest stay due for the next one.
func TestTriggeredExitsHonoursTheLimit(t *testing.T) {
	o := testBook()
	for i := 0; i < 3; i++ {
		fillBracketBuy(t, o, uuid.New(), 100, 1, 150, 0)
	}
	tradeAt(t, o, 150)

	if due := o.TriggeredExits(2); len(due) != 2 {
		t.Fatalf("TriggeredExits(2) returned %d ids, want 2", len(due))
	}
	if !o.HasTriggered() {
		t.Fatal("limiting the sweep must not hide the exits it left behind")
	}
}
