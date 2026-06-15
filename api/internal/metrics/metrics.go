// Package metrics defines the api-process Prometheus instruments (subsystem me_api_*) and a
// thin, nil-safe recorder injected into the HTTP middleware and the order publisher. Keeping
// registration and label order in one place lets call sites use the allocation-free positional
// fast path without re-stating label keys. See docs/observability.md §3.1.
package metrics

import (
	"time"

	"github.com/alex99y/matching-engine/common/pkg/observability"
)

const (
	metricHTTPRequests    = "http_requests_total"
	metricHTTPDuration    = "http_request_duration_seconds"
	metricHTTPInFlight    = "http_requests_in_flight"
	metricOrderPublish    = "order_publish_total"
	metricOrderPublishDur = "order_publish_duration_seconds"
)

// Publish result label values.
const (
	ResultSuccess = "success"
	ResultError   = "error"
)

// Label keys. Order is the contract for the positional fast-path methods below — values are
// passed in exactly this order.
var (
	httpLabelKeys    = []string{"method", "route", "status"}
	publishLabelKeys = []string{"market", "result"}
	marketLabelKeys  = []string{"market"}
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

	return &ApiMetrics{
		httpRequests:    httpRequests,
		httpDuration:    httpDuration,
		httpInFlight:    httpInFlight,
		orderPublish:    orderPublish,
		orderPublishDur: orderPublishDur,
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
