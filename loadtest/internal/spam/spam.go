// Package spam generates background order flow that the measured tests are not scored on. It
// runs a maker/taker account pool so matches happen between two distinct counterparties rather
// than relying on a single account crossing its own orders — self-trade prevention doesn't exist
// in this engine yet (see TODO.md), and a single self-trading account would silently stop
// producing matches the day it's implemented.
package spam

import (
	"context"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alex99y/matching-engine/loadtest/internal/client"
	"github.com/alex99y/matching-engine/loadtest/internal/harness"
)

// matchOpPercent is the target share of spam operations that produce a match (~80% per the
// project plan): each is a maker GTC limit order immediately crossed by a taker IOC limit order
// at the same price. The remainder rest a GTC order priced well outside that crossing band so it
// contributes book depth/load without matching.
const matchOpPercent = 80

const (
	crossingSpreadTicks = 20 // matching pairs are priced within ±this many price_quantum ticks of center
	restingOffsetTicks  = 200
)

// avgOrdersPerOp is the expected number of order-create calls per spam tick: a matching tick
// fires two (maker + taker), a resting-only tick fires one. Start() divides the tick rate by
// this so the configured rate means orders/sec actually hitting the api, not ticks/sec.
var avgOrdersPerOp = float64(matchOpPercent)/100*2 + float64(100-matchOpPercent)/100*1

// bigintMax mirrors the BIGINT storage ceiling core/pkg/order_events_queue enforces on
// price*quantity (see ValidateOrderEvent) — kept local since that constant isn't exported, and
// e2e can't import core/internal either way (different module, no shared internal visibility).
const bigintMax uint64 = math.MaxInt64

type Spammer struct {
	client    *client.Client
	market    *client.Market
	marketRef string
	makers    []*harness.Account
	takers    []*harness.Account
	rate      int
	qty       uint64
	refCenter uint64

	attempted     atomic.Int64
	succeeded     atomic.Int64
	rejected      atomic.Int64
	matchAttempts atomic.Int64

	startedAt time.Time
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

type Stats struct {
	Attempted     int64
	Succeeded     int64
	Rejected      int64
	MatchAttempts int64
	AchievedRate  float64 // successful ops/sec actually sustained
}

func New(env *harness.Environment, rate int) *Spammer {
	return &Spammer{
		client:    env.Client,
		market:    env.Market,
		marketRef: env.MarketRef,
		makers:    env.Makers,
		takers:    env.Takers,
		rate:      rate,
		qty:       OrderQty(env.Market),
		refCenter: ReferencePrice(env.Market),
	}
}

// OrderQty is the fixed order size used for every spam op (and, for consistency, for measured
// test orders too — see the cmd packages). Exported so measurement code prices/sizes its own
// orders identically to the spam traffic it's running alongside.
func OrderQty(m *client.Market) uint64 {
	if m.MinOrderSize > 0 {
		return m.MinOrderSize
	}
	if m.AmountQuantum > 0 {
		return m.AmountQuantum
	}
	return 1
}

// ReferencePrice picks a price safely below the point where price*qty (qty == OrderQty(m)) would
// overflow the engine's BIGINT notional ceiling, then rounds down to a valid tick. Exported so
// measurement code can price test orders within (or deliberately outside) the same band spam
// trades in.
func ReferencePrice(m *client.Market) uint64 {
	qty := OrderQty(m)
	quantum := m.PriceQuantum
	if quantum == 0 {
		quantum = 1
	}

	maxSafe := bigintMax / qty / uint64(crossingSpreadTicks+restingOffsetTicks+1)
	center := maxSafe / 10 // generous margin below the overflow boundary
	if center < quantum {
		center = quantum
	}
	return center - (center % quantum)
}

// Start launches the worker pool in the background and returns immediately. rate <= 0 (level 0)
// starts no workers — a valid, deliberate "no spam" baseline.
func (s *Spammer) Start(ctx context.Context) {
	spamCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.startedAt = time.Now()

	if s.rate <= 0 {
		return
	}

	tickRate := float64(s.rate) / avgOrdersPerOp
	workerCount := int(tickRate/50) + 1
	if workerCount > 200 {
		workerCount = 200
	}
	interval := time.Duration(float64(workerCount) * float64(time.Second) / tickRate)

	for i := 0; i < workerCount; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-spamCtx.Done():
					return
				case <-ticker.C:
					s.doOp(spamCtx)
				}
			}
		}()
	}
}

// Stop cancels the worker pool and blocks until every in-flight op finishes.
func (s *Spammer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Spammer) Stats() Stats {
	elapsed := time.Since(s.startedAt).Seconds()
	succeeded := s.succeeded.Load()
	var achieved float64
	if elapsed > 0 {
		achieved = float64(succeeded) / elapsed
	}
	return Stats{
		Attempted:     s.attempted.Load(),
		Succeeded:     succeeded,
		Rejected:      s.rejected.Load(),
		MatchAttempts: s.matchAttempts.Load(),
		AchievedRate:  achieved,
	}
}

func (s *Spammer) doOp(ctx context.Context) {
	s.attempted.Add(1)
	if rand.IntN(100) < matchOpPercent {
		s.doMatchingPair(ctx)
	} else {
		s.doRestingOnly(ctx)
	}
}

func (s *Spammer) doMatchingPair(ctx context.Context) {
	if len(s.makers) == 0 || len(s.takers) == 0 {
		return
	}
	maker := s.makers[rand.IntN(len(s.makers))]
	taker := s.takers[rand.IntN(len(s.takers))]
	price := s.crossingPrice()

	makerSide, takerSide := client.Sell, client.Buy
	if rand.IntN(2) == 0 {
		makerSide, takerSide = client.Buy, client.Sell
	}

	if _, err := s.client.CreateOrder(ctx, maker.Token, client.CreateOrderRequest{
		OrderSide: makerSide, OrderType: client.Limit, TimeInForce: client.GoodTillCancel,
		Market: s.marketRef, Price: price, Quantity: s.qty,
	}); err != nil {
		s.rejected.Add(1)
		return
	}
	s.succeeded.Add(1)

	if _, err := s.client.CreateOrder(ctx, taker.Token, client.CreateOrderRequest{
		OrderSide: takerSide, OrderType: client.Limit, TimeInForce: client.ImmediateOrCancel,
		Market: s.marketRef, Price: price, Quantity: s.qty,
	}); err != nil {
		s.rejected.Add(1)
		return
	}
	s.succeeded.Add(1)
	s.matchAttempts.Add(1)
}

func (s *Spammer) doRestingOnly(ctx context.Context) {
	if len(s.makers) == 0 {
		return
	}
	maker := s.makers[rand.IntN(len(s.makers))]
	side := client.Buy
	if rand.IntN(2) == 0 {
		side = client.Sell
	}

	_, err := s.client.CreateOrder(ctx, maker.Token, client.CreateOrderRequest{
		OrderSide: side, OrderType: client.Limit, TimeInForce: client.GoodTillCancel,
		Market: s.marketRef, Price: s.restingPrice(side), Quantity: s.qty,
	})
	if err != nil {
		s.rejected.Add(1)
		return
	}
	s.succeeded.Add(1)
}

func (s *Spammer) crossingPrice() uint64 {
	quantum := s.quantum()
	ticks := rand.IntN(2*crossingSpreadTicks+1) - crossingSpreadTicks // [-N, N]
	return OffsetPrice(s.refCenter, ticks, quantum)
}

// restingPrice pushes far enough outside the crossing band that it should never cross a
// matching-pair order, regardless of which side of the pair it lands on.
func (s *Spammer) restingPrice(side client.OrderSide) uint64 {
	quantum := s.quantum()
	if side == client.Buy {
		return OffsetPrice(s.refCenter, -restingOffsetTicks, quantum)
	}
	return OffsetPrice(s.refCenter, restingOffsetTicks, quantum)
}

func (s *Spammer) quantum() uint64 {
	if s.market.PriceQuantum == 0 {
		return 1
	}
	return s.market.PriceQuantum
}

// OffsetPrice moves center by ticks*quantum (ticks may be negative), clamped to never go below
// one quantum. Exported so measurement code can place orders a known distance from the same
// reference price spam uses — e.g. deliberately outside any crossing band.
func OffsetPrice(center uint64, ticks int, quantum uint64) uint64 {
	delta := int64(ticks) * int64(quantum)
	price := int64(center) + delta
	if price < int64(quantum) {
		price = int64(quantum)
	}
	return uint64(price)
}

// Cleanup cancels every order the maker/taker pool left resting (maker legs a taker never
// crossed, plus the deliberately non-matching resting orders) so a repeated run starts from a
// clean book instead of accumulating stale orders across invocations.
func (s *Spammer) Cleanup(ctx context.Context) error {
	for _, acc := range append(append([]*harness.Account{}, s.makers...), s.takers...) {
		if err := harness.CancelAllOpenOrders(ctx, s.client, acc, s.marketRef); err != nil {
			return err
		}
	}
	return nil
}
