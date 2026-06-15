package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The no-validation fast-path methods (positional label values) must record the same value as
// the validated Labels-based methods, without allocating/sorting a map.
func TestCounterFastPath(t *testing.T) {
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{Registerer: prometheus.NewRegistry()})
	counter, err := metrics.RegisterCounter(CounterDefinition{
		Name: "orders_total", Help: "orders", LabelKeys: []string{"market"},
	})
	if err != nil {
		t.Fatalf("register counter: %v", err)
	}

	counter.IncValues("BTC-USDT")
	counter.AddValues(4, "BTC-USDT")

	if got := testutil.ToFloat64(counter.vec.WithLabelValues("BTC-USDT")); got != 5 {
		t.Fatalf("counter fast path = %v, want 5", got)
	}
}

func TestGaugeFastPath(t *testing.T) {
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{Registerer: prometheus.NewRegistry()})
	gauge, err := metrics.RegisterGauge(GaugeDefinition{
		Name: "book_orders", Help: "resting orders", LabelKeys: []string{"market", "side"},
	})
	if err != nil {
		t.Fatalf("register gauge: %v", err)
	}

	gauge.SetValues(10, "BTC-USDT", "buy")
	gauge.IncValues("BTC-USDT", "buy")
	gauge.DecValues("BTC-USDT", "buy")
	gauge.AddValues(2, "BTC-USDT", "buy")

	if got := testutil.ToFloat64(gauge.vec.WithLabelValues("BTC-USDT", "buy")); got != 12 {
		t.Fatalf("gauge fast path = %v, want 12", got)
	}
}

// Bind resolves the label set once; the returned handle records into the same series as the
// positional methods, so a hot caller can hold it and call Inc()/Observe() allocation-free.
func TestBindHandlesShareSeries(t *testing.T) {
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{Registerer: prometheus.NewRegistry()})
	counter, err := metrics.RegisterCounter(CounterDefinition{
		Name: "trades_total", Help: "trades", LabelKeys: []string{"market"},
	})
	if err != nil {
		t.Fatalf("register counter: %v", err)
	}

	bound := counter.Bind("ETH-USDT")
	bound.Inc()
	counter.IncValues("ETH-USDT") // same series as the bound handle

	if got := testutil.ToFloat64(counter.vec.WithLabelValues("ETH-USDT")); got != 2 {
		t.Fatalf("bound counter = %v, want 2", got)
	}

	hist, err := metrics.RegisterHistogram(HistogramDefinition{
		Name: "batch_seconds", Help: "batch latency", LabelKeys: []string{"market"},
		Buckets: []float64{0.001, 0.01, 0.1},
	})
	if err != nil {
		t.Fatalf("register histogram: %v", err)
	}

	obs := hist.Bind("ETH-USDT")
	obs.Observe(0.005)
	hist.ObserveValues(0.05, "ETH-USDT")

	if got := testutil.CollectAndCount(hist.vec); got != 1 {
		t.Fatalf("histogram series count = %d, want 1", got)
	}
}

// A positional call with the wrong number of values is a programmer error and must panic —
// that is the documented signal in place of the cold path's returned error.
func TestFastPathPanicsOnArityMismatch(t *testing.T) {
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{Registerer: prometheus.NewRegistry()})
	counter, err := metrics.RegisterCounter(CounterDefinition{
		Name: "arity_total", Help: "arity", LabelKeys: []string{"market"},
	})
	if err != nil {
		t.Fatalf("register counter: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on label arity mismatch")
		}
	}()
	counter.IncValues("BTC-USDT", "extra") // 2 values, 1 label key
}
