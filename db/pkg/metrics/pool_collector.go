package metrics

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

// poolCollector exposes database/sql pool stats at scrape time. sql.DB.Stats() is a cheap
// snapshot, so reading it inside Collect is the idiomatic way to surface it — and it lets the
// cumulative WaitCount/WaitDuration be emitted as true counters via ConstMetric.
type poolCollector struct {
	db *sql.DB

	open         *prometheus.Desc
	inUse        *prometheus.Desc
	idle         *prometheus.Desc
	waitCount    *prometheus.Desc
	waitDuration *prometheus.Desc
}

func newPoolCollector(db *sql.DB, service string) *poolCollector {
	constLabels := prometheus.Labels{"service": service}
	return &poolCollector{
		db: db,
		open: prometheus.NewDesc("me_db_pool_connections_open",
			"Open connections (in use + idle).", nil, constLabels),
		inUse: prometheus.NewDesc("me_db_pool_connections_in_use",
			"Connections currently in use.", nil, constLabels),
		idle: prometheus.NewDesc("me_db_pool_connections_idle",
			"Idle connections in the pool.", nil, constLabels),
		waitCount: prometheus.NewDesc("me_db_pool_wait_count_total",
			"Total number of times a caller waited for a connection.", nil, constLabels),
		waitDuration: prometheus.NewDesc("me_db_pool_wait_duration_seconds_total",
			"Cumulative time blocked waiting for a connection, in seconds.", nil, constLabels),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.open
	ch <- c.inUse
	ch <- c.idle
	ch <- c.waitCount
	ch <- c.waitDuration
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.db.Stats()
	ch <- prometheus.MustNewConstMetric(c.open, prometheus.GaugeValue, float64(s.OpenConnections))
	ch <- prometheus.MustNewConstMetric(c.inUse, prometheus.GaugeValue, float64(s.InUse))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(s.Idle))
	ch <- prometheus.MustNewConstMetric(c.waitCount, prometheus.CounterValue, float64(s.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitDuration, prometheus.CounterValue, s.WaitDuration.Seconds())
}
