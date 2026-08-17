// publisher_test.go covers only the connection-independent parts of OrderCommandPublisher:
// constructor validation and the not-found path in Publish. Actually publishing (Publish's
// success path, which drives a real *order_events_queue.OrdersEventsQueue / *amqp091.Channel)
// needs a live broker — consistent with the rest of this codebase (see
// common/pkg/rabbitmq/client_test.go), that I/O-bound behavior isn't unit tested here.
package orderqueue_test

import (
	"context"
	"errors"
	"testing"

	"github.com/alex99y/matching-engine/api/internal/metrics"
	"github.com/alex99y/matching-engine/api/pkg/orderqueue"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/prometheus/client_golang/prometheus"
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

func TestNewOrderCommandPublisherPanicsOnNilLogger(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a nil logger")
		}
	}()
	orderqueue.NewOrderCommandPublisher(nil, &rabbitmq.RabbitMQClient{}, nil, nil)
}

func TestNewOrderCommandPublisherPanicsOnNilClient(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a nil rabbitMQClient")
		}
	}()
	orderqueue.NewOrderCommandPublisher(logger.NewLogger(logger.Error), nil, nil, nil)
}

// With an empty marketRefs slice, the constructor never dials a queue, so this exercises
// real construction without needing a live broker.
func TestPublishMarketQueueNotFound(t *testing.T) {
	pub := orderqueue.NewOrderCommandPublisher(
		logger.NewLogger(logger.Error), &rabbitmq.RabbitMQClient{}, nil, nil,
	)

	event, err := order_events_queue.NewCancelOrderEvent(&order_events_queue.CancelOrderEvent{
		MarketRef: "ETH-USDT",
	})
	if err != nil {
		t.Fatalf("NewCancelOrderEvent: %v", err)
	}

	err = pub.Publish(context.Background(), "msg-1", "ETH-USDT", event)
	if !errors.Is(err, orderqueue.ErrMarketQueueNotFound) {
		t.Fatalf("err = %v, want wrapping ErrMarketQueueNotFound", err)
	}
}

func TestPublishMarketQueueNotFoundRecordsErrorMetric(t *testing.T) {
	am, reg := newAPIMetrics(t)
	pub := orderqueue.NewOrderCommandPublisher(
		logger.NewLogger(logger.Error), &rabbitmq.RabbitMQClient{}, nil, am,
	)

	event, err := order_events_queue.NewCancelOrderEvent(&order_events_queue.CancelOrderEvent{
		MarketRef: "ETH-USDT",
	})
	if err != nil {
		t.Fatalf("NewCancelOrderEvent: %v", err)
	}
	if err := pub.Publish(context.Background(), "msg-1", "ETH-USDT", event); err == nil {
		t.Fatal("expected an error")
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var got float64 = -1
	for _, mf := range mfs {
		if mf.GetName() != "me_api_order_publish_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, p := range m.GetLabel() {
				labels[p.GetName()] = p.GetValue()
			}
			if labels["market"] == "ETH-USDT" && labels["result"] == metrics.ResultError {
				got = m.GetCounter().GetValue()
			}
		}
	}
	if got != 1 {
		t.Fatalf("order_publish_total{market=ETH-USDT,result=error} = %v, want 1", got)
	}
}
