//go:build e2e

package streams

import (
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/stream"
)

// S4 — the candle stream pushes each trade into the bucket in progress.
//
// Subscribes at the one-minute interval and then trades.
// Expect: an opening snapshot of the current bucket, aligned to the interval, followed by a
// candle.trade carrying the price, quantity, taker side and a timestamp inside that bucket.
func TestCandleStreamPushesTradesIntoTheCurrentBucket(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	feed, err := stream.ConnectCandles(ctx, env.Cfg.APIURL, env.Market.Ref, client.Interval1m)
	if err != nil {
		t.Fatalf("subscribe to the candle stream: %v", err)
	}
	defer feed.Close()

	snapshot, err := feed.WaitForSnapshot(ctx)
	if err != nil {
		t.Fatalf("no opening candle snapshot: %v", err)
	}
	if snapshot.Interval != client.Interval1m {
		t.Fatalf("snapshot reports interval %d, want %d", snapshot.Interval, client.Interval1m)
	}
	if snapshot.BucketStart%client.Interval1m != 0 {
		t.Fatalf("bucket_start %d is not aligned to the %ds interval",
			snapshot.BucketStart, client.Interval1m)
	}
	if snapshot.High < snapshot.Low {
		t.Fatalf("opening snapshot has high %d below low %d", snapshot.High, snapshot.Low)
	}

	price, qty := env.Band(t), env.MinQty()
	env.Trade(t, ctx, maker, taker, price, qty)

	trade, err := feed.WaitForTrade(ctx, price)
	if err != nil {
		t.Fatalf("the trade at %d never reached the candle stream: %v", price, err)
	}
	if trade.Quantity != qty {
		t.Fatalf("candle trade reports quantity %d, want %d", trade.Quantity, qty)
	}
	if trade.TakerSide != string(client.Buy) {
		t.Fatalf("candle trade reports taker side %q, want %q", trade.TakerSide, client.Buy)
	}
	// The trade has to belong to a bucket at or after the one we opened on.
	if bucket := trade.Time - trade.Time%client.Interval1m; bucket < snapshot.BucketStart {
		t.Fatalf("trade at %d falls in bucket %d, before the snapshot's %d",
			trade.Time, bucket, snapshot.BucketStart)
	}
}

// S4 — the candle stream refuses what it cannot serve.
//
// Expect: an unsupported interval and an unknown market are both rejected at connect time
// rather than opening a stream that never carries anything.
func TestCandleStreamRejectsBadSubscriptions(t *testing.T) {
	ctx := env.Context(t)

	if s, err := stream.ConnectCandles(ctx, env.Cfg.APIURL, env.Market.Ref, 42); err == nil {
		s.Close()
		t.Fatal("interval 42 was accepted, want a 400 at connect")
	}
	if s, err := stream.ConnectCandles(ctx, env.Cfg.APIURL, "NOPE-NOPE", client.Interval1m); err == nil {
		s.Close()
		t.Fatal("an unknown market was accepted, want a 404 at connect")
	}
}

// S2 — the public market stream also refuses an unknown market at connect.
func TestMarketStreamRejectsUnknownMarket(t *testing.T) {
	ctx := env.Context(t)

	if s, err := stream.ConnectMarket(ctx, env.Cfg.APIURL, "NOPE-NOPE", 0); err == nil {
		s.Close()
		t.Fatal("subscribing to an unknown market succeeded, want a 404 at connect")
	}
}
