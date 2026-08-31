//go:build e2e

package marketdata

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// M4 — candles aggregate the trades in their window.
//
// Trades twice at different prices inside a window opened just beforehand, then reads the
// one-minute candles over exactly that window.
// Expect: open is the first price, close the last, high and low the extremes, and volume the
// summed base quantity. Candles are computed live from the matches table, so the window
// bounds — not the bucket boundary — are what keep other tests' trades out.
func TestCandlesAggregateTradesInTheWindow(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	// Open the window before the first trade so nothing earlier can fall inside it.
	from := time.Now().Add(-time.Second).Unix()

	low := env.Band(t)
	high := low + 2*env.Market.PriceQuantum
	qty := env.MinQty()

	env.Trade(t, ctx, maker, taker, low, qty)  // first  → open
	env.Trade(t, ctx, maker, taker, high, qty) // second → close

	var candles client.Candles
	assert.Eventually(t, ctx, func() error {
		to := time.Now().Add(time.Second).Unix()
		c, err := env.Client.GetCandles(ctx, env.Market.Ref, client.Interval1m, from, to)
		if err != nil {
			return err
		}
		if len(c.Candles) == 0 {
			return errNoCandles{}
		}
		if total := totalVolume(t, c); total < 2*qty {
			return errVolumeShort{got: total, want: 2 * qty}
		}
		candles = c
		return nil
	})

	if candles.Interval != client.Interval1m {
		t.Fatalf("candles report interval %d, want %d", candles.Interval, client.Interval1m)
	}

	// The two trades may straddle a minute boundary, so read open/close across the buckets
	// rather than assuming a single one.
	first, last := candles.Candles[0], candles.Candles[len(candles.Candles)-1]
	if got := parseAmount(t, first.Open); got != low {
		t.Fatalf("open = %d, want the first trade's price %d", got, low)
	}
	if got := parseAmount(t, last.Close); got != high {
		t.Fatalf("close = %d, want the last trade's price %d", got, high)
	}
	if got := highestOf(t, candles); got != high {
		t.Fatalf("high = %d, want %d", got, high)
	}
	if got := lowestOf(t, candles); got != low {
		t.Fatalf("low = %d, want %d", got, low)
	}
	if got := totalVolume(t, candles); got != 2*qty {
		t.Fatalf("volume = %d, want %d (both fills)", got, 2*qty)
	}

	for _, c := range candles.Candles {
		if c.BucketStart%client.Interval1m != 0 {
			t.Fatalf("bucket_start %d is not aligned to the %ds interval", c.BucketStart, client.Interval1m)
		}
		o, h, l, cl := parseAmount(t, c.Open), parseAmount(t, c.High), parseAmount(t, c.Low), parseAmount(t, c.Close)
		if h < l {
			t.Fatalf("bucket %d has high %d below low %d", c.BucketStart, h, l)
		}
		if o < l || o > h || cl < l || cl > h {
			t.Fatalf("bucket %d: open %d / close %d fall outside the %d-%d range", c.BucketStart, o, cl, l, h)
		}
	}
}

// M4 — the candles endpoint refuses requests it cannot serve meaningfully.
func TestCandlesRejectBadRequests(t *testing.T) {
	ctx := env.Context(t)
	now := time.Now().Unix()

	t.Run("unsupported interval", func(t *testing.T) {
		if _, err := env.Client.GetCandles(ctx, env.Market.Ref, 42, now-3600, now); err == nil {
			t.Fatal("interval 42 was accepted, want 400")
		} else if got := client.Status(err); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
		}
	})

	t.Run("inverted range", func(t *testing.T) {
		if _, err := env.Client.GetCandles(ctx, env.Market.Ref, client.Interval1m, now, now-3600); err == nil {
			t.Fatal("a range ending before it starts was accepted, want 400")
		} else if got := client.Status(err); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
		}
	})

	t.Run("range wider than the cap", func(t *testing.T) {
		// The handler caps a request at 1000 buckets.
		if _, err := env.Client.GetCandles(ctx, env.Market.Ref, client.Interval1m, now-1001*60-60, now); err == nil {
			t.Fatal("an over-wide range was accepted, want 400")
		} else if got := client.Status(err); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
		}
	})

	t.Run("unknown market", func(t *testing.T) {
		if _, err := env.Client.GetCandles(ctx, "NOPE-NOPE", client.Interval1m, now-3600, now); err == nil {
			t.Fatal("an unknown market was accepted, want 404")
		} else if got := client.Status(err); got != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", got, http.StatusNotFound)
		}
	})
}

// Candle OHLCV values arrive as decimal strings, not numbers — they are raw uint64 quanta
// that would lose precision as JSON numbers in a JavaScript client.
func parseAmount(t *testing.T, s string) uint64 {
	t.Helper()

	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		t.Fatalf("candle amount %q is not a uint64: %v", s, err)
	}
	return v
}

func totalVolume(t *testing.T, c client.Candles) uint64 {
	t.Helper()

	var total uint64
	for _, candle := range c.Candles {
		total += parseAmount(t, candle.Volume)
	}
	return total
}

func highestOf(t *testing.T, c client.Candles) uint64 {
	t.Helper()

	var high uint64
	for _, candle := range c.Candles {
		if v := parseAmount(t, candle.High); v > high {
			high = v
		}
	}
	return high
}

func lowestOf(t *testing.T, c client.Candles) uint64 {
	t.Helper()

	var low uint64
	for _, candle := range c.Candles {
		v := parseAmount(t, candle.Low)
		if low == 0 || v < low {
			low = v
		}
	}
	return low
}

type errNoCandles struct{}

func (errNoCandles) Error() string { return "no candles in the window yet" }

type errVolumeShort struct{ got, want uint64 }

func (e errVolumeShort) Error() string {
	return "window volume " + strconv.FormatUint(e.got, 10) + ", waiting for " + strconv.FormatUint(e.want, 10)
}
