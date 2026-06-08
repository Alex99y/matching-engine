package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestPrometheusServerExposesRegisteredMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewPrometheusMetrics(PrometheusMetricsConfig{
		Registerer: registry,
	})

	counter, err := metrics.RegisterCounter(CounterDefinition{
		Name:      "test_requests_total",
		Help:      "test requests",
		LabelKeys: []string{"status"},
	})
	if err != nil {
		t.Fatalf("expected counter register to succeed, got: %v", err)
	}

	gauge, err := metrics.RegisterGauge(GaugeDefinition{
		Name:      "test_queue_depth",
		Help:      "test queue depth",
		LabelKeys: []string{"network"},
	})
	if err != nil {
		t.Fatalf("expected gauge register to succeed, got: %v", err)
	}

	if err := counter.Inc(Labels{"status": "ok"}); err != nil {
		t.Fatalf("expected counter increment to succeed, got: %v", err)
	}

	if err := gauge.Set(5, Labels{"network": "evm"}); err != nil {
		t.Fatalf("expected gauge set to succeed, got: %v", err)
	}

	server := NewPrometheusServer(0, metrics, nil)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response, err := server.app.Test(request)
	if err != nil {
		t.Fatalf("expected /metrics request to succeed, got: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got: %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("expected reading response body to succeed, got: %v", err)
	}

	metricsOutput := string(body)
	if !strings.Contains(metricsOutput, "test_requests_total") {
		t.Fatal("expected /metrics output to include test_requests_total")
	}

	if !strings.Contains(metricsOutput, "test_queue_depth") {
		t.Fatal("expected /metrics output to include test_queue_depth")
	}
}
