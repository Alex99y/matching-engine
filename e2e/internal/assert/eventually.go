// Package assert holds the domain assertions the e2e suites share: polling for
// asynchronous state, balance conservation, order-book depth, and order/stream outcomes.
// Functions take testing.TB and fail via Fatalf; the pure logic behind them is factored out
// and unit-tested.
package assert

import (
	"context"
	"testing"
	"time"
)

// Eventually polls fn every 100ms until it returns nil, then returns. If ctx expires first
// it fails the test with fn's last error. ctx must carry a deadline (typically
// cfg.SettleTimeout) — the API→broker→core→DB path is asynchronous, so nothing is readable
// the instant POST /orders returns.
func Eventually(t testing.TB, ctx context.Context, fn func() error) {
	t.Helper()
	const tick = 100 * time.Millisecond

	var last error
	for {
		if last = fn(); last == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("assert.Eventually: condition not met before deadline: %v", last)
			return
		case <-time.After(tick):
		}
	}
}
