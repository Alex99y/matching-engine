// Package metrics defines the matching-engine Prometheus instruments (subsystem me_core_*) and
// a per-market bundle of pre-bound handles. The matcher runs one processor per market, so each
// processor binds its label set once at construction (BindMarket) and the per-order hot path then
// costs a single atomic add with no map lookup or allocation. See docs/observability.md §3.3.
package metrics

import (
	"time"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricOrdersReceived    = "orders_received_total"
	metricOrdersProcessed   = "orders_processed_total"
	metricTrades            = "trades_total"
	metricBatchSize         = "batch_size"
	metricBatchDuration     = "batch_duration_seconds"
	metricBatches           = "batches_total"
	metricReserveRejections = "reserve_rejections_total"
	metricPoisonIsolations  = "poison_isolations_total"
	metricDeadLetters       = "dead_letters_total"
	metricBookRebuilds      = "book_rebuilds_total"
	metricBookOrders        = "book_orders"
	metricBookBestPrice     = "book_best_price"
	// Event-log publisher (docs/event-log.md). published/dropped are per market; publish errors are
	// global (the publisher goroutine knows only the routing key, not the market).
	metricStreamPublished     = "stream_events_published_total"
	metricStreamDropped       = "stream_events_dropped_total"
	metricStreamPublishErrors = "stream_publish_errors_total"
)

// Outcome label values for orders_processed. The first four mirror the persisted order status so
// result.NewOrders[].Status maps directly; "rejected" is synthetic (could not reserve funds).
const (
	OutcomeOpen            = repository.OrderStatusOpen
	OutcomeFilled          = repository.OrderStatusFilled
	OutcomePartiallyFilled = repository.OrderStatusPartiallyFilled
	OutcomeCancelled       = repository.OrderStatusCancelled
	OutcomeRejected        = "rejected"
)

// Batch result label values for batches_total.
const (
	BatchCommitted      = "committed"
	BatchTransientFail  = "transient_fail"
	BatchPoisonIsolated = "poison_isolated"
)

// Side label values for the book gauges.
const (
	SideBuy  = "buy"
	SideSell = "sell"
)

var (
	marketLabel      = []string{"market"}
	marketOutcome    = []string{"market", "outcome"}
	marketResult     = []string{"market", "result"}
	marketSide       = []string{"market", "side"}
	marketType       = []string{"market", "type"}
	batchSizeBuckets = []float64{1, 2, 4, 8, 16, 32, 64, 96, 128}
	batchDurBuckets  = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1}

	// streamEventTypes are the per-market event-log event types (the routing-key trailing word and
	// the published_total "type" label). Pre-bound per market so recording is allocation-free.
	streamEventTypes = []string{
		string(marketdata.EventTrade), string(marketdata.EventBook), string(marketdata.EventOrder),
		string(marketdata.EventSnapshot), string(marketdata.EventHeartbeat),
	}
)

// CoreMetrics holds the registered me_core_* instruments. Bind per market via BindMarket.
type CoreMetrics struct {
	ordersReceived    *observability.CounterMetric
	ordersProcessed   *observability.CounterMetric
	trades            *observability.CounterMetric
	batchSize         *observability.HistogramMetric
	batchDuration     *observability.HistogramMetric
	batches           *observability.CounterMetric
	reserveRejections *observability.CounterMetric
	poisonIsolations  *observability.CounterMetric
	deadLetters       *observability.CounterMetric
	bookRebuilds      *observability.CounterMetric
	bookOrders        *observability.GaugeMetric
	bookBestPrice     *observability.GaugeMetric
	streamPublished   *observability.CounterMetric
	streamDropped     *observability.CounterMetric
	streamPublishErr  *observability.CounterMetric
}

func NewCoreMetrics(pm *observability.PrometheusMetrics) (*CoreMetrics, error) {
	c := &CoreMetrics{}
	var err error
	if c.ordersReceived, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricOrdersReceived, Help: "Open orders accepted into the engine.", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.ordersProcessed, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricOrdersProcessed, Help: "Orders processed in a committed batch, by outcome.", LabelKeys: marketOutcome,
	}); err != nil {
		return nil, err
	}
	if c.trades, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricTrades, Help: "Executed trades (fills).", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.batchSize, err = pm.RegisterHistogram(observability.HistogramDefinition{
		Name: metricBatchSize, Help: "Events per committed micro-batch.", LabelKeys: marketLabel, Buckets: batchSizeBuckets,
	}); err != nil {
		return nil, err
	}
	if c.batchDuration, err = pm.RegisterHistogram(observability.HistogramDefinition{
		Name: metricBatchDuration, Help: "Match+commit latency per micro-batch, in seconds.", LabelKeys: marketLabel, Buckets: batchDurBuckets,
	}); err != nil {
		return nil, err
	}
	if c.batches, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricBatches, Help: "Micro-batch outcomes (committed/transient_fail/poison_isolated).", LabelKeys: marketResult,
	}); err != nil {
		return nil, err
	}
	if c.reserveRejections, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricReserveRejections, Help: "Orders rejected at balance reservation (insufficient funds).", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.poisonIsolations, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricPoisonIsolations, Help: "Batches that fell into per-order poison isolation.", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.deadLetters, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricDeadLetters, Help: "Orders dead-lettered after exceeding the failure cap.", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.bookRebuilds, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricBookRebuilds, Help: "Book rebuilds (hydrations) triggered by a failed batch.", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.bookOrders, err = pm.RegisterGauge(observability.GaugeDefinition{
		Name: metricBookOrders, Help: "Resting orders in the book, by side.", LabelKeys: marketSide,
	}); err != nil {
		return nil, err
	}
	if c.bookBestPrice, err = pm.RegisterGauge(observability.GaugeDefinition{
		Name: metricBookBestPrice, Help: "Best price in the book, by side (0 when empty).", LabelKeys: marketSide,
	}); err != nil {
		return nil, err
	}
	if c.streamPublished, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricStreamPublished, Help: "Event-log events handed to the publisher, by market and type.", LabelKeys: marketType,
	}); err != nil {
		return nil, err
	}
	if c.streamDropped, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricStreamDropped, Help: "Event-log events dropped on a full publisher buffer (consumers re-snapshot).", LabelKeys: marketLabel,
	}); err != nil {
		return nil, err
	}
	if c.streamPublishErr, err = pm.RegisterCounter(observability.CounterDefinition{
		Name: metricStreamPublishErrors, Help: "Event-log broker publish failures (after the publisher's reopen-retry).",
	}); err != nil {
		return nil, err
	}
	return c, nil
}

// IncPublishError records a broker publish failure. Called from the shared publisher goroutine,
// which has no market context — so this counter is global. Nil-safe.
func (c *CoreMetrics) IncPublishError() {
	if c == nil {
		return
	}
	c.streamPublishErr.IncValues()
}

// MarketMetrics is the per-market bundle of concrete, pre-bound handles. A nil *MarketMetrics is
// valid and disables recording (so a processor built without metrics still runs).
type MarketMetrics struct {
	received   prometheus.Counter
	trades     prometheus.Counter
	reserveRej prometheus.Counter
	poison     prometheus.Counter
	deadLetter prometheus.Counter
	rebuilds   prometheus.Counter
	batchSize  prometheus.Observer
	batchDur   prometheus.Observer
	processed  map[string]prometheus.Counter // outcome -> counter
	batches    map[string]prometheus.Counter // result -> counter
	bookOrders map[string]prometheus.Gauge   // side -> gauge
	bookBest   map[string]prometheus.Gauge   // side -> gauge
	published  map[string]prometheus.Counter // event type -> counter
	dropped    prometheus.Counter
}

// BindMarket resolves every label set for one market once. Returns nil if c is nil.
func (c *CoreMetrics) BindMarket(market string) *MarketMetrics {
	if c == nil {
		return nil
	}
	return &MarketMetrics{
		received:   c.ordersReceived.Bind(market),
		trades:     c.trades.Bind(market),
		reserveRej: c.reserveRejections.Bind(market),
		poison:     c.poisonIsolations.Bind(market),
		deadLetter: c.deadLetters.Bind(market),
		rebuilds:   c.bookRebuilds.Bind(market),
		batchSize:  c.batchSize.Bind(market),
		batchDur:   c.batchDuration.Bind(market),
		processed: map[string]prometheus.Counter{
			OutcomeOpen:            c.ordersProcessed.Bind(market, OutcomeOpen),
			OutcomeFilled:          c.ordersProcessed.Bind(market, OutcomeFilled),
			OutcomePartiallyFilled: c.ordersProcessed.Bind(market, OutcomePartiallyFilled),
			OutcomeCancelled:       c.ordersProcessed.Bind(market, OutcomeCancelled),
			OutcomeRejected:        c.ordersProcessed.Bind(market, OutcomeRejected),
		},
		batches: map[string]prometheus.Counter{
			BatchCommitted:      c.batches.Bind(market, BatchCommitted),
			BatchTransientFail:  c.batches.Bind(market, BatchTransientFail),
			BatchPoisonIsolated: c.batches.Bind(market, BatchPoisonIsolated),
		},
		bookOrders: map[string]prometheus.Gauge{
			SideBuy:  c.bookOrders.Bind(market, SideBuy),
			SideSell: c.bookOrders.Bind(market, SideSell),
		},
		bookBest: map[string]prometheus.Gauge{
			SideBuy:  c.bookBestPrice.Bind(market, SideBuy),
			SideSell: c.bookBestPrice.Bind(market, SideSell),
		},
		published: bindStreamTypes(c.streamPublished, market),
		dropped:   c.streamDropped.Bind(market),
	}
}

// bindStreamTypes pre-binds the published counter for every event type of one market.
func bindStreamTypes(counter *observability.CounterMetric, market string) map[string]prometheus.Counter {
	m := make(map[string]prometheus.Counter, len(streamEventTypes))
	for _, t := range streamEventTypes {
		m[t] = counter.Bind(market, t)
	}
	return m
}

func (m *MarketMetrics) IncReceived() {
	if m == nil {
		return
	}
	m.received.Inc()
}

func (m *MarketMetrics) AddTrades(n int) {
	if m == nil || n == 0 {
		return
	}
	m.trades.Add(float64(n))
}

func (m *MarketMetrics) IncProcessed(outcome string) {
	if m == nil {
		return
	}
	if c, ok := m.processed[outcome]; ok {
		c.Inc()
	}
}

// AddRejected records reservation rejections both as a dedicated counter and as the "rejected"
// outcome of orders_processed, so the outcome mix stays complete.
func (m *MarketMetrics) AddRejected(n int) {
	if m == nil || n <= 0 {
		return
	}
	m.reserveRej.Add(float64(n))
	if c, ok := m.processed[OutcomeRejected]; ok {
		c.Add(float64(n))
	}
}

func (m *MarketMetrics) IncPoison() {
	if m == nil {
		return
	}
	m.poison.Inc()
}

func (m *MarketMetrics) IncDeadLetter() {
	if m == nil {
		return
	}
	m.deadLetter.Inc()
}

func (m *MarketMetrics) IncRebuild() {
	if m == nil {
		return
	}
	m.rebuilds.Inc()
}

func (m *MarketMetrics) ObserveBatch(size int, d time.Duration) {
	if m == nil {
		return
	}
	m.batchSize.Observe(float64(size))
	m.batchDur.Observe(d.Seconds())
}

func (m *MarketMetrics) IncBatch(result string) {
	if m == nil {
		return
	}
	if c, ok := m.batches[result]; ok {
		c.Inc()
	}
}

// SetBook updates the depth and best-price gauges for one side. best is ignored (set to 0) when
// hasBest is false (empty side).
func (m *MarketMetrics) SetBook(side string, orders int, best uint64, hasBest bool) {
	if m == nil {
		return
	}
	if g, ok := m.bookOrders[side]; ok {
		g.Set(float64(orders))
	}
	if g, ok := m.bookBest[side]; ok {
		if hasBest {
			g.Set(float64(best))
		} else {
			g.Set(0)
		}
	}
}

// IncStreamPublished counts one event-log event enqueued to the publisher, by type.
func (m *MarketMetrics) IncStreamPublished(eventType string) {
	if m == nil {
		return
	}
	if c, ok := m.published[eventType]; ok {
		c.Inc()
	}
}

// IncStreamDropped counts one event dropped on a full publisher buffer.
func (m *MarketMetrics) IncStreamDropped() {
	if m == nil {
		return
	}
	m.dropped.Inc()
}
