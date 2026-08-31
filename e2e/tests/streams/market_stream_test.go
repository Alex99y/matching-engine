//go:build e2e

package streams

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// S2 — the public stream reports what the book does.
//
// Subscribes, rests a buy, cancels it, then trades at a second price.
// Expect: a delta announcing the new level, a delta zeroing it on cancel, and a trade frame
// carrying the price, quantity and taker side. Clients never receive the internal (epoch,
// seq) the API and core sync on, so ordering is asserted through the deltas themselves.
func TestMarketStreamPublishesBookDeltasAndTrades(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	feed, err := stream.ConnectMarket(ctx, env.Cfg.APIURL, env.Market.Ref, 0)
	if err != nil {
		t.Fatalf("subscribe to the market stream: %v", err)
	}
	defer feed.Close()

	// The first frame is always the snapshot of the book as it stands.
	if _, err := feed.WaitForSnapshot(ctx); err != nil {
		t.Fatalf("no opening snapshot: %v", err)
	}

	restPrice, qty := env.Band(t), env.MinQty()

	// --- a level appears ---
	orderID := env.Send(t, ctx, acc, fixtures.LimitBuy(env.Market, restPrice, qty))
	if _, err := feed.WaitForBook(ctx, string(client.Buy), restPrice, qty); err != nil {
		t.Fatalf("no book delta for the new bid at %d: %v", restPrice, err)
	}

	// --- and disappears again ---
	if err := env.Client.CancelOrder(ctx, acc.LoginToken, orderID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := feed.WaitForBook(ctx, string(client.Buy), restPrice, 0); err != nil {
		t.Fatalf("no book delta emptying the bid at %d — a removed level must be published "+
			"as quantity 0, not simply omitted: %v", restPrice, err)
	}

	// --- a trade is announced ---
	tradePrice := env.Band(t)
	env.Trade(t, ctx, acc, taker, tradePrice, qty)

	trade, err := feed.WaitForTrade(ctx, tradePrice)
	if err != nil {
		t.Fatalf("no trade frame at %d: %v", tradePrice, err)
	}
	if trade.Quantity != qty {
		t.Fatalf("trade frame reports quantity %d, want %d", trade.Quantity, qty)
	}
	if trade.TakerSide != string(client.Buy) {
		t.Fatalf("trade frame reports taker side %q, want %q — the crossing order was the buy",
			trade.TakerSide, client.Buy)
	}
}

// S3 — the opening snapshot agrees with the REST depth read.
//
// Rests two orders, then connects fresh and compares the snapshot against GET /depth.
// Expect: the snapshot contains the resting levels at their true quantities, is ordered
// best-first on both sides, and matches what the depth endpoint reports at the same moment —
// both are served from the same cache, so a divergence means the cache is inconsistent.
func TestMarketStreamSnapshotMatchesRESTDepth(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	tick := env.Market.PriceQuantum
	top := env.Band(t)
	bid, ask := top-tick, top
	qty := env.MinQty()

	env.Rest(t, ctx, acc, fixtures.LimitBuy(env.Market, bid, qty))
	env.Rest(t, ctx, acc, fixtures.LimitSell(env.Market, ask, qty))

	// Connect after the orders are in, so the snapshot has to contain them.
	feed, err := stream.ConnectMarket(ctx, env.Cfg.APIURL, env.Market.Ref, 0)
	if err != nil {
		t.Fatalf("subscribe to the market stream: %v", err)
	}
	defer feed.Close()

	snapshot, err := feed.WaitForSnapshot(ctx)
	if err != nil {
		t.Fatalf("no opening snapshot: %v", err)
	}
	if snapshot.Market != env.Market.Ref {
		t.Fatalf("snapshot is for market %q, want %q", snapshot.Market, env.Market.Ref)
	}

	if got := levelQty(snapshot.Bids, bid); got != qty {
		t.Fatalf("snapshot bid at %d = %d, want %d", bid, got, qty)
	}
	if got := levelQty(snapshot.Asks, ask); got != qty {
		t.Fatalf("snapshot ask at %d = %d, want %d", ask, got, qty)
	}

	for i := 1; i < len(snapshot.Bids); i++ {
		if snapshot.Bids[i-1].Price <= snapshot.Bids[i].Price {
			t.Fatalf("snapshot bids are not best-first: %d then %d",
				snapshot.Bids[i-1].Price, snapshot.Bids[i].Price)
		}
	}
	for i := 1; i < len(snapshot.Asks); i++ {
		if snapshot.Asks[i-1].Price >= snapshot.Asks[i].Price {
			t.Fatalf("snapshot asks are not best-first: %d then %d",
				snapshot.Asks[i-1].Price, snapshot.Asks[i].Price)
		}
	}

	// The one-shot REST read is the same cache seen through a different door.
	depth, err := env.Client.GetDepth(ctx, env.Market.Ref, 0)
	if err != nil {
		t.Fatalf("get depth: %v", err)
	}
	for _, l := range []struct {
		side  client.OrderSide
		price uint64
	}{{client.Buy, bid}, {client.Sell, ask}} {
		var streamed uint64
		if l.side == client.Buy {
			streamed = levelQty(snapshot.Bids, l.price)
		} else {
			streamed = levelQty(snapshot.Asks, l.price)
		}
		var rested uint64
		levels := depth.Bids
		if l.side == client.Sell {
			levels = depth.Asks
		}
		for _, d := range levels {
			if d.Price == l.price {
				rested = d.Quantity
			}
		}
		if streamed != rested {
			t.Fatalf("%s @ %d: snapshot says %d, REST depth says %d",
				l.side, l.price, streamed, rested)
		}
	}
}

// S5 — reconnecting is how a client resynchronises.
//
// Connects, rests an order, then drops the connection and reconnects.
// Expect: the second connection's snapshot already contains the order placed during the first
// — there is no sequence number to resume from on the wire, so a fresh snapshot is the whole
// recovery mechanism, and it must be current.
func TestReconnectingYieldsACurrentSnapshot(t *testing.T) {
	ctx := env.Context(t)
	acc := env.NewFundedAccount(t)

	first, err := stream.ConnectMarket(ctx, env.Cfg.APIURL, env.Market.Ref, 0)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	before, err := first.WaitForSnapshot(ctx)
	if err != nil {
		t.Fatalf("no opening snapshot: %v", err)
	}

	price, qty := env.Band(t), env.MinQty()
	if got := levelQty(before.Bids, price); got != 0 {
		t.Fatalf("this test's price band is already occupied: %d @ %d", got, price)
	}

	env.Rest(t, ctx, acc, fixtures.LimitBuy(env.Market, price, qty))
	if _, err := first.WaitForBook(ctx, string(client.Buy), price, qty); err != nil {
		t.Fatalf("the first connection missed its own delta: %v", err)
	}
	first.Close()

	second, err := stream.ConnectMarket(ctx, env.Cfg.APIURL, env.Market.Ref, 0)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer second.Close()

	after, err := second.WaitForSnapshot(ctx)
	if err != nil {
		t.Fatalf("no snapshot after reconnecting: %v", err)
	}
	if got := levelQty(after.Bids, price); got != qty {
		t.Fatalf("the reconnect snapshot has %d @ %d, want %d — a reconnecting client would "+
			"never learn about the order it missed", got, price, qty)
	}
}

func levelQty(levels []stream.Level, price uint64) uint64 {
	for _, l := range levels {
		if l.Price == price {
			return l.Quantity
		}
	}
	return 0
}
