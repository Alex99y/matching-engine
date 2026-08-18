// latency-match measures the time between an order being sent and it being matched — the first
// event with status filled or partially_filled observed on the measured account's private order
// stream (first match, not necessarily fully filled).
//
// Each sample self-supplies its own counterparty: a GTC resting leg immediately crossed by an
// IOC leg from the measured account, both at the same price, so a fill is available to measure
// even at level 0 (no spam, so no external liquidity). The two legs aren't guaranteed to land in
// order — the IOC leg occasionally beats its own resting leg into the book and cancels unfilled;
// see correlate.DeadOrder / the "dead" column in the report. The resting leg itself is unmeasured
// plumbing; only the crossing IOC order is tracked.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/loadtest/internal/client"
	"github.com/alex99y/matching-engine/loadtest/internal/correlate"
	"github.com/alex99y/matching-engine/loadtest/internal/report"
	"github.com/alex99y/matching-engine/loadtest/internal/scenario"
	"github.com/alex99y/matching-engine/loadtest/internal/spam"
	"github.com/alex99y/matching-engine/loadtest/internal/stream"
)

const testName = "latency-match"

func main() {
	scenario.Run(testName, run)
}

func isMatch(e stream.OrderEvent) bool {
	return e.Status == stream.StatusFilled || e.Status == stream.StatusPartiallyFilled
}

func run(ctx context.Context, deps scenario.Deps) ([]report.Metric, error) {
	env := deps.Env
	cfg := deps.Cfg

	tracker := correlate.NewTracker(isMatch)
	go correlate.Pump(ctx, env.Stream.Events(), tracker)

	qty := spam.OrderQty(env.Market)
	price := spam.ReferencePrice(env.Market)

	interval := cfg.Duration / time.Duration(cfg.SampleCount)
	if interval <= 0 {
		interval = time.Millisecond
	}

	for i := 0; i < cfg.SampleCount; i++ {
		if ctx.Err() != nil {
			break
		}
		makerSide, takerSide := client.Sell, client.Buy
		if i%2 == 1 {
			makerSide, takerSide = client.Buy, client.Sell
		}

		if _, err := env.Client.CreateOrder(ctx, env.Measured.Token, client.CreateOrderRequest{
			OrderSide: makerSide, OrderType: client.Limit, TimeInForce: client.GoodTillCancel,
			Market: env.MarketRef, Price: price, Quantity: qty,
		}); err != nil {
			fmt.Printf("[%s] sample %d: counterparty leg failed: %v\n", testName, i, err)
			continue
		}

		sentAt := time.Now()
		orderID, err := env.Client.CreateOrder(ctx, env.Measured.Token, client.CreateOrderRequest{
			OrderSide: takerSide, OrderType: client.Limit, TimeInForce: client.ImmediateOrCancel,
			Market: env.MarketRef, Price: price, Quantity: qty,
		})
		if err != nil {
			fmt.Printf("[%s] sample %d: measured order failed: %v\n", testName, i, err)
			continue
		}
		tracker.Track(orderID, sentAt)

		select {
		case <-time.After(interval):
		case <-ctx.Done():
		}
	}

	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
	}
	tracker.Sweep(time.Now())

	return []report.Metric{{
		Name:     "match",
		Samples:  tracker.Samples(),
		Dead:     tracker.Dead(),
		Timeouts: tracker.Timeouts(),
	}}, nil
}
