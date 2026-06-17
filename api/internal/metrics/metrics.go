// Package metrics defines the api-process Prometheus instruments (subsystem me_api_*) and a
// thin, nil-safe recorder injected into the HTTP middleware and the order publisher. Keeping
// registration and label order in one place lets call sites use the allocation-free positional
// fast path without re-stating label keys. See docs/observability.md §3.1.
package metrics

import (
	"time"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/observability"
)

// streamEventTypes are the event-log event types (mirrors the publisher), used to pre-create the
// consumer's per-type series at zero — see PrimeStream.
var streamEventTypes = []string{
	string(marketdata.EventTrade), string(marketdata.EventBook), string(marketdata.EventOrder),
	string(marketdata.EventSnapshot), string(marketdata.EventHeartbeat),
}

const (
	metricHTTPRequests    = "http_requests_total"
	metricHTTPDuration    = "http_request_duration_seconds"
	metricHTTPInFlight    = "http_requests_in_flight"
	metricOrderPublish    = "order_publish_total"
	metricOrderPublishDur = "order_publish_duration_seconds"
	// Event-log consumer (docs/event-log.md, Phase C).
	metricStreamReceived       = "stream_events_received_total"
	metricStreamResyncs        = "stream_resyncs_total"
	metricStreamPublicClients  = "stream_public_clients"
	metricStreamPrivateUsers   = "stream_private_users"
	metricStreamClientsDropped = "stream_clients_dropped_total"
)

// Publish result label values.
const (
	ResultSuccess = "success"
	ResultError   = "error"
)

// Resync reason label values (why a market cache fell out of sync).
const (
	ResyncGap   = "gap"
	ResyncEpoch = "epoch"
)

// Stream client kind label values.
const (
	KindPublic  = "public"
	KindPrivate = "private"
)

// Label keys. Order is the contract for the positional fast-path methods below — values are
// passed in exactly this order.
var (
	httpLabelKeys    = []string{"method", "route", "status"}
	publishLabelKeys = []string{"market", "result"}
	marketLabelKeys  = []string{"market"}
	marketTypeKeys   = []string{"market", "type"}
	marketReasonKeys = []string{"market", "reason"}
	streamKindKeys   = []string{"kind"}
)

var (
	httpDurationBuckets    = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	publishDurationBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1}
)

// ApiMetrics holds the registered handles. A nil *ApiMetrics is valid and disables recording,
// so the HTTP server and publisher can run without metrics wired (e.g. in tests).
type ApiMetrics struct {
	httpRequests    *observability.CounterMetric
	httpDuration    *observability.HistogramMetric
	httpInFlight    *observability.GaugeMetric
	orderPublish    *observability.CounterMetric
	orderPublishDur *observability.HistogramMetric
	streamReceived  *observability.CounterMetric
	streamResyncs   *observability.CounterMetric
	streamPublicCli *observability.GaugeMetric
	streamPrivUsers *observability.GaugeMetric
	streamCliDrop   *observability.CounterMetric
}

// NewApiMetrics registers the me_api_* instruments on pm and returns the recorder.
func NewApiMetrics(pm *observability.PrometheusMetrics) (*ApiMetrics, error) {
	httpRequests, err := pm.RegisterCounter(observability.CounterDefinition{
		Name: metricHTTPRequests, Help: "Total HTTP requests handled, by method/route/status.", LabelKeys: httpLabelKeys,
	})
	if err != nil {
		return nil, err
	}
	httpDuration, err := pm.RegisterHistogram(observability.HistogramDefinition{
		Name: metricHTTPDuration, Help: "HTTP request latency in seconds, by method/route/status.", LabelKeys: httpLabelKeys, Buckets: httpDurationBuckets,
	})
	if err != nil {
		return nil, err
	}
	httpInFlight, err := pm.RegisterGauge(observability.GaugeDefinition{
		Name: metricHTTPInFlight, Help: "HTTP requests currently being served.",
	})
	if err != nil {
		return nil, err
	}
	orderPublish, err := pm.RegisterCounter(observability.CounterDefinition{
		Name: metricOrderPublish, Help: "Order command publishes to RabbitMQ, by market and result.", LabelKeys: publishLabelKeys,
	})
	if err != nil {
		return nil, err
	}
	orderPublishDur, err := pm.RegisterHistogram(observability.HistogramDefinition{
		Name: metricOrderPublishDur, Help: "Latency of publishing an order command to RabbitMQ.", LabelKeys: marketLabelKeys, Buckets: publishDurationBuckets,
	})
	if err != nil {
		return nil, err
	}
	streamReceived, err := pm.RegisterCounter(observability.CounterDefinition{
		Name: metricStreamReceived, Help: "Event-log events consumed from the exchange, by market and type.", LabelKeys: marketTypeKeys,
	})
	if err != nil {
		return nil, err
	}
	streamResyncs, err := pm.RegisterCounter(observability.CounterDefinition{
		Name: metricStreamResyncs, Help: "Market book caches that fell out of sync (and re-sync from the next snapshot), by reason.", LabelKeys: marketReasonKeys,
	})
	if err != nil {
		return nil, err
	}
	streamPublicCli, err := pm.RegisterGauge(observability.GaugeDefinition{
		Name: metricStreamPublicClients, Help: "Connected public SSE clients, by market.", LabelKeys: marketLabelKeys,
	})
	if err != nil {
		return nil, err
	}
	streamPrivUsers, err := pm.RegisterGauge(observability.GaugeDefinition{
		Name: metricStreamPrivateUsers, Help: "Distinct users with an active private stream binding on this instance.",
	})
	if err != nil {
		return nil, err
	}
	streamCliDrop, err := pm.RegisterCounter(observability.CounterDefinition{
		Name: metricStreamClientsDropped, Help: "SSE clients dropped for lagging (buffer full), by kind.", LabelKeys: streamKindKeys,
	})
	if err != nil {
		return nil, err
	}

	return &ApiMetrics{
		httpRequests:    httpRequests,
		httpDuration:    httpDuration,
		httpInFlight:    httpInFlight,
		orderPublish:    orderPublish,
		orderPublishDur: orderPublishDur,
		streamReceived:  streamReceived,
		streamResyncs:   streamResyncs,
		streamPublicCli: streamPublicCli,
		streamPrivUsers: streamPrivUsers,
		streamCliDrop:   streamCliDrop,
	}, nil
}

// IncInFlight / DecInFlight bracket a request being served.
func (m *ApiMetrics) IncInFlight() {
	if m == nil {
		return
	}
	m.httpInFlight.IncValues()
}

func (m *ApiMetrics) DecInFlight() {
	if m == nil {
		return
	}
	m.httpInFlight.DecValues()
}

// RecordHTTPRequest records one completed request. status is the numeric code as a string.
func (m *ApiMetrics) RecordHTTPRequest(method, route, status string, latency time.Duration) {
	if m == nil {
		return
	}
	m.httpRequests.IncValues(method, route, status)
	m.httpDuration.ObserveValues(latency.Seconds(), method, route, status)
}

// RecordOrderPublish records one publish attempt (success or error) and its latency.
func (m *ApiMetrics) RecordOrderPublish(market, result string, latency time.Duration) {
	if m == nil {
		return
	}
	m.orderPublish.IncValues(market, result)
	m.orderPublishDur.ObserveValues(latency.Seconds(), market)
}

// RecordStreamEvent counts one event-log envelope consumed from the exchange.
func (m *ApiMetrics) RecordStreamEvent(market, eventType string) {
	if m == nil {
		return
	}
	m.streamReceived.IncValues(market, eventType)
}

// RecordResync counts one market cache falling out of sync (reason ResyncGap or ResyncEpoch).
func (m *ApiMetrics) RecordResync(market, reason string) {
	if m == nil {
		return
	}
	m.streamResyncs.IncValues(market, reason)
}

// IncPublicClients / DecPublicClients track connected public SSE clients per market.
func (m *ApiMetrics) IncPublicClients(market string) {
	if m == nil {
		return
	}
	m.streamPublicCli.IncValues(market)
}

func (m *ApiMetrics) DecPublicClients(market string) {
	if m == nil {
		return
	}
	m.streamPublicCli.DecValues(market)
}

// IncPrivateUsers / DecPrivateUsers track distinct users with an active private binding (bound on a
// user's first connection, unbound on their last).
func (m *ApiMetrics) IncPrivateUsers() {
	if m == nil {
		return
	}
	m.streamPrivUsers.IncValues()
}

func (m *ApiMetrics) DecPrivateUsers() {
	if m == nil {
		return
	}
	m.streamPrivUsers.DecValues()
}

// IncClientDropped counts one SSE client dropped for lagging (kind KindPublic or KindPrivate).
func (m *ApiMetrics) IncClientDropped(kind string) {
	if m == nil {
		return
	}
	m.streamCliDrop.IncValues(kind)
}

// PrimeStream pre-creates every bounded event-log series at zero for the served markets, so the
// dashboards show flat-zero lines from startup rather than "No data" until the first event (and a
// healthy, never-resyncing system still renders a 0 line instead of a blank panel). Mirrors the core
// publisher's pre-binding. Call once at startup; nil-safe.
func (m *ApiMetrics) PrimeStream(markets []string) {
	if m == nil {
		return
	}
	m.streamPrivUsers.SetValues(0)
	for _, kind := range []string{KindPublic, KindPrivate} {
		m.streamCliDrop.AddValues(0, kind)
	}
	for _, market := range markets {
		m.streamPublicCli.SetValues(0, market)
		for _, t := range streamEventTypes {
			m.streamReceived.AddValues(0, market, t)
		}
		for _, reason := range []string{ResyncGap, ResyncEpoch} {
			m.streamResyncs.AddValues(0, market, reason)
		}
	}
}
