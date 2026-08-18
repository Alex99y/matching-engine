// latency-ack measures the time between an order being sent and it being received by the
// matching engine — the first event of any kind (open, filled, partially_filled, or rejected)
// observed on the measured account's private order stream. A taker order that fills immediately
// never emits "open", so "first event, whatever it is" is the only definition of "received" that
// covers every order outcome.
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

const testName = "latency-ack"

func main() {
	scenario.Run(testName, run)
}

func run(ctx context.Context, deps scenario.Deps) ([]report.Metric, error) {
	env := deps.Env
	cfg := deps.Cfg

	tracker := correlate.NewTracker(func(stream.OrderEvent) bool { return true })
	go correlate.Pump(ctx, env.Stream.Events(), tracker)

	qty := spam.OrderQty(env.Market)
	price := spam.ReferencePrice(env.Market) // inside the spam pool's crossing band: realistic contention

	interval := cfg.Duration / time.Duration(cfg.SampleCount)
	if interval <= 0 {
		interval = time.Millisecond
	}

	for i := 0; i < cfg.SampleCount; i++ {
		if ctx.Err() != nil {
			break
		}
		side := client.Buy
		if i%2 == 1 {
			side = client.Sell
		}

		sentAt := time.Now()
		orderID, err := env.Client.CreateOrder(ctx, env.Measured.Token, client.CreateOrderRequest{
			OrderSide: side, OrderType: client.Limit, TimeInForce: client.GoodTillCancel,
			Market: env.MarketRef, Price: price, Quantity: qty,
		})
		if err != nil {
			fmt.Printf("[%s] sample %d: create order failed: %v\n", testName, i, err)
			continue
		}
		tracker.Track(orderID, sentAt)

		select {
		case <-time.After(interval):
		case <-ctx.Done():
		}
	}

	waitForStragglers(ctx)
	tracker.Sweep(time.Now())

	return []report.Metric{{
		Name:     "ack",
		Samples:  tracker.Samples(),
		Dead:     tracker.Dead(),
		Timeouts: tracker.Timeouts(),
	}}, nil
}

// waitForStragglers gives the last few in-flight events a chance to arrive before Sweep declares
// their orders timed out.
func waitForStragglers(ctx context.Context) {
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
	}
}
