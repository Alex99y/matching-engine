package observability

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrometheusMetricsRegisterCounterAndRecord(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: registry,
	})

	counter, err := metrics.RegisterCounter(CounterDefinition{
		Name:      "deposits_total",
		Help:      "total deposits",
		LabelKeys: []string{"network"},
	})
	if err != nil {
		t.Fatalf("expected counter registration to succeed, got: %v", err)
	}

	if err := counter.Inc(Labels{"network": "evm"}); err != nil {
		t.Fatalf("expected counter increment to succeed, got: %v", err)
	}

	if err := counter.CustomInc(2, Labels{"network": "evm"}); err != nil {
		t.Fatalf("expected counter add to succeed, got: %v", err)
	}

	value := testutil.ToFloat64(counter.vec.WithLabelValues("evm"))
	if value != 3 {
		t.Fatalf("expected counter value 3, got: %v", value)
	}
}

func TestPrometheusMetricsRegisterGaugeAndRecord(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: registry,
	})

	gauge, err := metrics.RegisterGauge(GaugeDefinition{
		Name:      "pending_forwards",
		Help:      "pending forwards",
		LabelKeys: []string{"network"},
	})
	if err != nil {
		t.Fatalf("expected gauge registration to succeed, got: %v", err)
	}

	if err := gauge.Set(10, Labels{"network": "btc"}); err != nil {
		t.Fatalf("expected gauge set to succeed, got: %v", err)
	}
	if err := gauge.Dec(Labels{"network": "btc"}); err != nil {
		t.Fatalf("expected gauge dec to succeed, got: %v", err)
	}
	if err := gauge.CustomAdd(5, Labels{"network": "btc"}); err != nil {
		t.Fatalf("expected gauge add to succeed, got: %v", err)
	}

	value := testutil.ToFloat64(gauge.vec.WithLabelValues("btc"))
	if value != 14 {
		t.Fatalf("expected gauge value 14, got: %v", value)
	}
}

func TestPrometheusMetricsRegisterHistogramAndRecord(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: registry,
	})

	histogram, err := metrics.RegisterHistogram(HistogramDefinition{
		Name:      "manager_duration_seconds",
		Help:      "manager processing duration",
		LabelKeys: []string{"network"},
		Buckets:   []float64{0.1, 0.5, 1},
	})
	if err != nil {
		t.Fatalf("expected histogram registration to succeed, got: %v", err)
	}

	if err := histogram.Observe(0.2, Labels{"network": "sol"}); err != nil {
		t.Fatalf("expected histogram observe to succeed, got: %v", err)
	}
	if err := histogram.ObserveDuration(300*time.Millisecond, Labels{"network": "sol"}); err != nil {
		t.Fatalf("expected histogram observe duration to succeed, got: %v", err)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("expected gather to succeed, got: %v", err)
	}

	var sampleCount uint64
	for _, family := range families {
		if family.GetName() != "manager_duration_seconds" {
			continue
		}

		for _, metric := range family.GetMetric() {
			sampleCount += metric.GetHistogram().GetSampleCount()
		}
	}

	if sampleCount != 2 {
		t.Fatalf(
			"expected histogram sample count 2, got: %d",
			sampleCount,
		)
	}
}

func TestPrometheusMetricsDuplicateRegistrationReturnsError(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: registry,
	})

	def := CounterDefinition{
		Name:      "api_requests_total",
		Help:      "api requests",
		LabelKeys: []string{"route"},
	}

	if _, err := metrics.RegisterCounter(def); err != nil {
		t.Fatalf("expected first register to succeed, got: %v", err)
	}

	_, err := metrics.RegisterCounter(def)
	if err == nil {
		t.Fatal("expected duplicate register to fail")
	}

	if !errors.Is(err, ErrMetricAlreadyRegistered) {
		t.Fatalf("expected ErrMetricAlreadyRegistered, got: %v", err)
	}
}

func TestPrometheusMetricsInvalidLabelSetReturnsError(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: registry,
	})

	counter, err := metrics.RegisterCounter(CounterDefinition{
		Name:      "db_queries_total",
		Help:      "db queries",
		LabelKeys: []string{"operation", "status"},
	})
	if err != nil {
		t.Fatalf("expected register to succeed, got: %v", err)
	}

	err = counter.Inc(Labels{"operation": "select"})
	if err == nil {
		t.Fatal("expected invalid label set error")
	}

	if !errors.Is(err, ErrInvalidLabelSet) {
		t.Fatalf("expected ErrInvalidLabelSet, got: %v", err)
	}
}

func TestPrometheusMetricsGetUnknownMetricReturnsError(t *testing.T) {
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: prometheus.NewRegistry(),
	})

	_, err := metrics.GetCounter("unknown")
	if err == nil {
		t.Fatal("expected unknown metric lookup to fail")
	}

	if !errors.Is(err, ErrMetricNotFound) {
		t.Fatalf("expected ErrMetricNotFound, got: %v", err)
	}
}
