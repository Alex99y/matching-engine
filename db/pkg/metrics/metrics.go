// Package metrics defines the db-layer Prometheus instruments (subsystem me_db_*) shared by any
// process that hosts the repository (api and core). Every series carries a `service` label so the
// two connection pools are distinguishable on one dashboard. See docs/observability.md §3.2.
package metrics

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/lib/pq"
)

const (
	metricQueryDuration = "query_duration_seconds"
	metricQueryErrors   = "query_errors_total"
)

const (
	resultOK    = "ok"
	resultError = "error"
)

var (
	queryDurationLabels  = []string{"service", "operation", "result"}
	queryErrorLabels     = []string{"service", "operation", "class"}
	queryDurationBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25}
)

// DBMetrics records per-operation query latency and errors. service is baked in so call sites
// pass only operation + the error. A nil *DBMetrics is valid and disables recording.
type DBMetrics struct {
	service       string
	queryDuration *observability.HistogramMetric
	queryErrors   *observability.CounterMetric
}

// NewDBMetrics registers the me_db_* query instruments on pm and registers a scrape-time pool
// collector for db on pm's registry. Call once per process with the process's service name
// ("api" | "core") and its single *sql.DB.
func NewDBMetrics(pm *observability.PrometheusMetrics, db *sql.DB, service string) (*DBMetrics, error) {
	queryDuration, err := pm.RegisterHistogram(observability.HistogramDefinition{
		Name: metricQueryDuration, Help: "Repository query latency in seconds, by operation and result.",
		LabelKeys: queryDurationLabels, Buckets: queryDurationBuckets,
	})
	if err != nil {
		return nil, err
	}
	queryErrors, err := pm.RegisterCounter(observability.CounterDefinition{
		Name: metricQueryErrors, Help: "Repository query errors, by operation and SQLSTATE class.",
		LabelKeys: queryErrorLabels,
	})
	if err != nil {
		return nil, err
	}

	// Pool stats are a snapshot source (sql.DB.Stats()), so they are emitted at scrape time by a
	// custom collector rather than the event-driven wrapper — this keeps WaitCount/WaitDuration as
	// true counters (ConstMetric) with no goroutine or delta bookkeeping.
	if err := pm.GetRegistry().Register(newPoolCollector(db, service)); err != nil {
		return nil, err
	}

	return &DBMetrics{service: service, queryDuration: queryDuration, queryErrors: queryErrors}, nil
}

// ObserveQuery records one query's latency and, on error, increments the error counter by
// SQLSTATE class. Call it deferred with a pointer to the method's named error return so the final
// error is captured:
//
//	func (o *OrderRepository) GetOrdersByIDs(...) (_ []OrderRow, outErr error) {
//	    defer o.metrics.ObserveQuery("get_orders_by_ids", time.Now(), &outErr)
//
// Note: a "not found" that the method has translated to a domain sentinel is seen here only as a
// generic error (class "other"); raw sql.ErrNoRows is classed "no_rows" so dashboards can exclude it.
func (m *DBMetrics) ObserveQuery(operation string, start time.Time, errp *error) {
	if m == nil {
		return
	}
	result := resultOK
	if errp != nil && *errp != nil {
		result = resultError
	}
	m.queryDuration.ObserveValues(time.Since(start).Seconds(), m.service, operation, result)
	if result == resultError {
		m.queryErrors.IncValues(m.service, operation, errorClass(*errp))
	}
}

func errorClass(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if code := string(pqErr.Code); len(code) >= 2 {
			return code[:2]
		}
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "no_rows"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "context"
	default:
		return "other"
	}
}
