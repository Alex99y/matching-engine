// latency-cancel measures how long a cancel takes to land, in two spans (per the project plan):
//   - cancel_round_trip: from the cancel request's send time to the "cancelled" event.
//   - cancel_full_lifecycle: from the original order's send time to that same event.
//
// Each sample is priced well outside the spam pool's crossing band (see spam.OffsetPrice) so it
// rests rather than getting matched — a cancel test needs something to cancel.
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
	"github.com/google/uuid"
)

const testName = "latency-cancel"

const (
	cancelRetryBackoff = 50 * time.Millisecond
	maxCancelAttempts  = 5
	farOffsetTicks     = 500 // price ticks outside the spam pool's crossing band
)

func main() {
	scenario.Run(testName, run)
}

func isCancelled(e stream.OrderEvent) bool { return e.Status == stream.StatusCancelled }

func run(ctx context.Context, deps scenario.Deps) ([]report.Metric, error) {
	env := deps.Env
	cfg := deps.Cfg

	fullLifecycle := correlate.NewTracker(isCancelled)
	roundTrip := correlate.NewTracker(isCancelled)
	go correlate.Pump(ctx, env.Stream.Events(), fullLifecycle, roundTrip)

	qty := spam.OrderQty(env.Market)
	center := spam.ReferencePrice(env.Market)
	quantum := env.Market.PriceQuantum
	if quantum == 0 {
		quantum = 1
	}

	interval := cfg.Duration / time.Duration(cfg.SampleCount)
	if interval <= 0 {
		interval = time.Millisecond
	}

	raceLosses := 0
	for i := 0; i < cfg.SampleCount; i++ {
		if ctx.Err() != nil {
			break
		}
		side := client.Buy
		ticks := -farOffsetTicks
		if i%2 == 1 {
			side = client.Sell
			ticks = farOffsetTicks
		}
		price := spam.OffsetPrice(center, ticks, quantum)

		orderSentAt := time.Now()
		orderID, err := env.Client.CreateOrder(ctx, env.Measured.Token, client.CreateOrderRequest{
			OrderSide: side, OrderType: client.Limit, TimeInForce: client.GoodTillCancel,
			Market: env.MarketRef, Price: price, Quantity: qty,
		})
		if err != nil {
			fmt.Printf("[%s] sample %d: create order failed: %v\n", testName, i, err)
			continue
		}
		fullLifecycle.Track(orderID, orderSentAt)

		if !cancelWithRetry(ctx, env.Client, env.Measured.Token, orderID, roundTrip) {
			raceLosses++
		}

		select {
		case <-time.After(interval):
		case <-ctx.Done():
		}
	}

	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
	}
	fullLifecycle.Sweep(time.Now())
	roundTrip.Sweep(time.Now())

	fmt.Printf("[%s] cancel attempts that never succeeded (likely raced a fill): %d\n", testName, raceLosses)

	return []report.Metric{
		{Name: "cancel_full_lifecycle", Samples: fullLifecycle.Samples(), Dead: fullLifecycle.Dead(), Timeouts: fullLifecycle.Timeouts()},
		{Name: "cancel_round_trip", Samples: roundTrip.Samples(), Dead: roundTrip.Dead(), Timeouts: roundTrip.Timeouts()},
	}, nil
}

// cancelWithRetry absorbs the create/cancel race by retrying; only the successful attempt's send
// time is tracked.
func cancelWithRetry(ctx context.Context, c *client.Client, token string, orderID uuid.UUID, tracker *correlate.Tracker) bool {
	for attempt := 0; attempt < maxCancelAttempts; attempt++ {
		cancelSentAt := time.Now()
		if err := c.CancelOrder(ctx, token, orderID); err != nil {
			select {
			case <-time.After(cancelRetryBackoff):
			case <-ctx.Done():
				return false
			}
			continue
		}
		tracker.Track(orderID, cancelSentAt)
		return true
	}
	return false
}
