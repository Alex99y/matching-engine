// Package fixtures builds valid API requests for the market under test — prices as tick
// multiples, quantities as lot multiples — so a test states intent ("a resting buy two
// ticks below the ask") without hand-aligning numbers to the market's quanta.
package fixtures

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/harness"
)

// FundingCalls is the default per-symbol faucet-credit count for a test account. At the
// hardcoded faucet amounts (ETH 0.5, BTC 0.1, USDT 1000 per call) that is ~10 ETH / 2 BTC /
// 20k USDT — ample for any single test.
const FundingCalls = 20

type OrderOpt func(*client.NewOrder)

func WithTIF(t client.TimeInForce) OrderOpt { return func(o *client.NewOrder) { o.TimeInForce = t } }
func PostOnly() OrderOpt                    { return func(o *client.NewOrder) { o.PostOnly = true } }
func WithClientOrderID(id string) OrderOpt  { return func(o *client.NewOrder) { o.ClientOrderID = id } }
func ExpiresAt(unixSec int64) OrderOpt      { return func(o *client.NewOrder) { o.ExpiresAt = &unixSec } }

// Price is ticks × the market's price quantum (ticks >= 1).
func Price(m harness.MarketRules, ticks uint64) uint64 { return ticks * m.PriceQuantum }

// Qty is lots × the market's amount quantum. The caller keeps lots within
// [MinLots(m), MaxLots(m)].
func Qty(m harness.MarketRules, lots uint64) uint64 { return lots * m.AmountQuantum }

func MinLots(m harness.MarketRules) uint64 {
	if m.AmountQuantum == 0 {
		return 1
	}
	if lots := m.MinOrderSize / m.AmountQuantum; lots > 0 {
		return lots
	}
	return 1
}

func MaxLots(m harness.MarketRules) uint64 {
	if m.AmountQuantum == 0 || m.MaxOrderSize == 0 {
		return 0
	}
	return m.MaxOrderSize / m.AmountQuantum
}

// RestingBidPrice is the lowest buy price at which a minimum-size order still reserves a
// non-zero quote notional (price × qty ÷ BaseScale >= 1). It is orders of magnitude below any
// real quote, so such an order rests at the bottom of the book instead of crossing — the
// cheapest way for a test to get something resting without caring about the current market.
func RestingBidPrice(m harness.MarketRules) uint64 {
	qty := Qty(m, MinLots(m))
	if qty == 0 {
		return m.PriceQuantum
	}
	price := ceilDiv(m.BaseScale, qty)
	return ceilDiv(price, m.PriceQuantum) * m.PriceQuantum // round up onto the tick grid
}

// minAssertableNotional is the quote notional a minimum-size order needs before
// basis-point fees stop flooring to zero: at 1 bp the fee is notional/10000, so a million
// quanta leaves room for every fee tier the markets use.
const minAssertableNotional = 1_000_000

// TradablePrice is a price at which a minimum-size order carries a notional big enough to
// assert on (fills, fees, released reservations all round to non-zero). Tests offset from it
// to claim a price band of their own rather than hard-coding a number per market.
func TradablePrice(m harness.MarketRules) uint64 {
	qty := Qty(m, MinLots(m))
	if qty == 0 {
		return m.PriceQuantum
	}
	price := ceilDiv(minAssertableNotional*m.BaseScale, qty)
	return ceilDiv(price, m.PriceQuantum) * m.PriceQuantum
}

func ceilDiv(a, b uint64) uint64 {
	if b == 0 {
		return a
	}
	if a == 0 {
		return 1
	}
	return (a + b - 1) / b
}

// LimitBuy / LimitSell default to GTC. price and qty are raw quanta — use Price/Qty.
func LimitBuy(m harness.MarketRules, price, qty uint64, opts ...OrderOpt) client.NewOrder {
	return limit(m, client.Buy, price, qty, opts)
}

func LimitSell(m harness.MarketRules, price, qty uint64, opts ...OrderOpt) client.NewOrder {
	return limit(m, client.Sell, price, qty, opts)
}

func limit(m harness.MarketRules, side client.OrderSide, price, qty uint64, opts []OrderOpt) client.NewOrder {
	o := client.NewOrder{
		Market: m.Ref, Side: side, Type: client.Limit, TimeInForce: client.GTC,
		Price: price, Quantity: qty,
	}
	apply(&o, opts)
	return o
}

// MarketBuy is quote-denominated: budget is raw quote quanta. Defaults to IOC.
func MarketBuy(m harness.MarketRules, budget uint64, opts ...OrderOpt) client.NewOrder {
	o := client.NewOrder{
		Market: m.Ref, Side: client.Buy, Type: client.Market, TimeInForce: client.IOC,
		QuoteQty: &budget,
	}
	apply(&o, opts)
	return o
}

// MarketSell offers a raw base quantity. Defaults to IOC.
func MarketSell(m harness.MarketRules, qty uint64, opts ...OrderOpt) client.NewOrder {
	o := client.NewOrder{
		Market: m.Ref, Side: client.Sell, Type: client.Market, TimeInForce: client.IOC,
		Quantity: qty,
	}
	apply(&o, opts)
	return o
}

func apply(o *client.NewOrder, opts []OrderOpt) {
	for _, opt := range opts {
		opt(o)
	}
}

// ClientOrderID returns a fresh 32-hex-char id (the API requires 32–64).
func ClientOrderID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("fixtures: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
