package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// NewServiceRegistry returns a dedicated registry preloaded with the Go runtime and process
// collectors (goroutines, GC, memory, open fds, CPU). A dedicated registry — not the global
// default — is required because PrometheusServer.GetRegistry() type-asserts the registerer to
// *prometheus.Registry; passing the default registerer would panic there.
func NewServiceRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// NewSubsystemMetrics builds a PrometheusMetrics bound to reg for one namespace/subsystem.
// Several subsystems may share a single registry — e.g. the api process exports both
// me_api_* and me_db_* by creating two handles over the same registry — and one
// PrometheusServer over that registry exposes all of them on /metrics.
func NewSubsystemMetrics(reg *prometheus.Registry, namespace, subsystem string) *PrometheusMetrics {
	return NewPrometheusMetrics(PrometheusMetricsConfig{
		Namespace:  namespace,
		Subsystem:  subsystem,
		Registerer: reg,
	})
}
