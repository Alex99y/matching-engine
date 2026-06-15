package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/alex99y/matching-engine/api/internal/metrics"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func newAPIMetrics(t *testing.T) (*metrics.ApiMetrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	pm := observability.NewPrometheusMetrics(observability.PrometheusMetricsConfig{
		Namespace: "me", Subsystem: "api", Registerer: reg,
	})
	am, err := metrics.NewApiMetrics(pm)
	if err != nil {
		t.Fatalf("NewApiMetrics: %v", err)
	}
	return am, reg
}

// The request counter must be labeled by the registered route TEMPLATE (/api/v1/orders/:id),
// never the concrete path (/api/v1/orders/123), or cardinality is unbounded.
func TestAccessLogRecordsRouteTemplate(t *testing.T) {
	am, reg := newAPIMetrics(t)

	app := fiber.New()
	app.Use(AccessLog(logger.NewLogger(logger.Error), am))
	app.Get("/api/v1/orders/:id", func(c fiber.Ctx) error { return c.SendString("ok") })

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/orders/123", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	got := counterValue(t, reg, "me_api_http_requests_total",
		map[string]string{"method": "GET", "route": "/api/v1/orders/:id", "status": "200"})
	if got != 1 {
		t.Fatalf("request counter = %v, want 1 (route template label)", got)
	}
}

// A nil recorder disables recording and must not panic.
func TestAccessLogNilMetricsSafe(t *testing.T) {
	app := fiber.New()
	app.Use(AccessLog(logger.NewLogger(logger.Error), nil))
	app.Get("/health", func(c fiber.Ctx) error { return c.SendString("ok") })

	resp, err := app.Test(httptest.NewRequest("GET", "/health", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func counterValue(t *testing.T, g prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return -1
}

func labelsMatch(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	for _, p := range pairs {
		if want[p.GetName()] != p.GetValue() {
			return false
		}
	}
	return true
}
