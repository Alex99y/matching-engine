package observability

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Labels map[string]string

type CounterDefinition struct {
	Name      string
	Help      string
	LabelKeys []string
}

type GaugeDefinition struct {
	Name      string
	Help      string
	LabelKeys []string
}

type HistogramDefinition struct {
	Name      string
	Help      string
	LabelKeys []string
	Buckets   []float64
}

type PrometheusMetricsConfig struct {
	Namespace  string
	Subsystem  string
	Registerer prometheus.Registerer
}

type PrometheusMetrics struct {
	namespace  string
	subsystem  string
	registerer prometheus.Registerer

	counters   map[string]*CounterMetric
	gauges     map[string]*GaugeMetric
	histograms map[string]*HistogramMetric
}

type CounterMetric struct {
	vec       *prometheus.CounterVec
	labelKeys []string
}

type GaugeMetric struct {
	vec       *prometheus.GaugeVec
	labelKeys []string
}

type HistogramMetric struct {
	vec       *prometheus.HistogramVec
	labelKeys []string
}

var (
	ErrMetricAlreadyRegistered = errors.New("metric already registered")
	ErrMetricNotFound          = errors.New("metric not found")
	ErrInvalidLabelSet         = errors.New("invalid label set")
)

func NewPrometheusRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

func NewPrometheusMetrics(config PrometheusMetricsConfig) *PrometheusMetrics {
	registerer := config.Registerer
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	return &PrometheusMetrics{
		namespace:  config.Namespace,
		subsystem:  config.Subsystem,
		registerer: registerer,
		counters:   map[string]*CounterMetric{},
		gauges:     map[string]*GaugeMetric{},
		histograms: map[string]*HistogramMetric{},
	}
}

func (m *PrometheusMetrics) GetRegistry() *prometheus.Registry {
	return m.registerer.(*prometheus.Registry)
}

func (m *PrometheusMetrics) RegisterCounter(
	definition CounterDefinition,
) (*CounterMetric, error) {
	if _, exists := m.counters[definition.Name]; exists {
		return nil, fmt.Errorf("%w: %s", ErrMetricAlreadyRegistered, definition.Name)
	}

	vec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: m.namespace,
			Subsystem: m.subsystem,
			Name:      definition.Name,
			Help:      definition.Help,
		},
		definition.LabelKeys,
	)

	if err := m.registerer.Register(vec); err != nil {
		return nil, err
	}

	counter := &CounterMetric{
		vec:       vec,
		labelKeys: cloneLabelKeys(definition.LabelKeys),
	}

	m.counters[definition.Name] = counter

	return counter, nil
}

func (m *PrometheusMetrics) RegisterGauge(
	definition GaugeDefinition,
) (*GaugeMetric, error) {
	if _, exists := m.gauges[definition.Name]; exists {
		return nil, fmt.Errorf("%w: %s", ErrMetricAlreadyRegistered, definition.Name)
	}

	vec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Subsystem: m.subsystem,
			Name:      definition.Name,
			Help:      definition.Help,
		},
		definition.LabelKeys,
	)

	if err := m.registerer.Register(vec); err != nil {
		return nil, err
	}

	gauge := &GaugeMetric{
		vec:       vec,
		labelKeys: cloneLabelKeys(definition.LabelKeys),
	}

	m.gauges[definition.Name] = gauge

	return gauge, nil
}

func (m *PrometheusMetrics) RegisterHistogram(
	definition HistogramDefinition,
) (*HistogramMetric, error) {
	if _, exists := m.histograms[definition.Name]; exists {
		return nil, fmt.Errorf("%w: %s", ErrMetricAlreadyRegistered, definition.Name)
	}

	vec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: m.namespace,
			Subsystem: m.subsystem,
			Name:      definition.Name,
			Help:      definition.Help,
			Buckets:   definition.Buckets,
		},
		definition.LabelKeys,
	)

	if err := m.registerer.Register(vec); err != nil {
		return nil, err
	}

	histogram := &HistogramMetric{
		vec:       vec,
		labelKeys: cloneLabelKeys(definition.LabelKeys),
	}

	m.histograms[definition.Name] = histogram

	return histogram, nil
}

func (m *PrometheusMetrics) GetCounter(name string) (*CounterMetric, error) {
	counter, exists := m.counters[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrMetricNotFound, name)
	}
	return counter, nil
}

func (m *PrometheusMetrics) GetGauge(name string) (*GaugeMetric, error) {
	gauge, exists := m.gauges[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrMetricNotFound, name)
	}
	return gauge, nil
}

func (m *PrometheusMetrics) GetHistogram(
	name string,
) (*HistogramMetric, error) {
	histogram, exists := m.histograms[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrMetricNotFound, name)
	}
	return histogram, nil
}

func (m *PrometheusMetrics) PreRegister(
	counters []CounterDefinition,
	gauges []GaugeDefinition,
	histograms []HistogramDefinition,
) error {
	for _, definition := range counters {
		if _, err := m.RegisterCounter(definition); err != nil {
			return err
		}
	}

	for _, definition := range gauges {
		if _, err := m.RegisterGauge(definition); err != nil {
			return err
		}
	}

	for _, definition := range histograms {
		if _, err := m.RegisterHistogram(definition); err != nil {
			return err
		}
	}

	return nil
}

func (counter *CounterMetric) Inc(labels Labels) error {
	labelValues, err := orderedLabelValues(counter.labelKeys, labels)
	if err != nil {
		return err
	}
	counter.vec.WithLabelValues(labelValues...).Inc()
	return nil
}

func (counter *CounterMetric) CustomInc(value float64, labels Labels) error {
	labelValues, err := orderedLabelValues(counter.labelKeys, labels)
	if err != nil {
		return err
	}
	counter.vec.WithLabelValues(labelValues...).Add(value)
	return nil
}

// IncValues and AddValues are the no-validation fast path: label values are passed
// positionally in registration order, skipping the per-call map allocation, sort, and
// label-set check that Inc(Labels) performs. Use on hot paths (e.g. the per-order matcher
// loop). The number of values must match the registered label keys — a mismatch panics,
// which is the intended programmer-error signal and is covered by tests.
func (counter *CounterMetric) IncValues(values ...string) {
	counter.vec.WithLabelValues(values...).Inc()
}

func (counter *CounterMetric) AddValues(value float64, values ...string) {
	counter.vec.WithLabelValues(values...).Add(value)
}

// Bind resolves a fixed label set once and returns the concrete counter, so a hot caller
// with a stable label set (e.g. one market per processor) can hold the handle and call
// Inc()/Add() with zero per-call allocation. Same panic-on-mismatch contract as IncValues.
func (counter *CounterMetric) Bind(values ...string) prometheus.Counter {
	return counter.vec.WithLabelValues(values...)
}

func (gauge *GaugeMetric) Set(value float64, labels Labels) error {
	labelValues, err := orderedLabelValues(gauge.labelKeys, labels)
	if err != nil {
		return err
	}
	gauge.vec.WithLabelValues(labelValues...).Set(value)
	return nil
}

func (gauge *GaugeMetric) Inc(labels Labels) error {
	labelValues, err := orderedLabelValues(gauge.labelKeys, labels)
	if err != nil {
		return err
	}
	gauge.vec.WithLabelValues(labelValues...).Inc()
	return nil
}

func (gauge *GaugeMetric) Dec(labels Labels) error {
	labelValues, err := orderedLabelValues(gauge.labelKeys, labels)
	if err != nil {
		return err
	}
	gauge.vec.WithLabelValues(labelValues...).Dec()
	return nil
}

func (gauge *GaugeMetric) CustomAdd(value float64, labels Labels) error {
	labelValues, err := orderedLabelValues(gauge.labelKeys, labels)
	if err != nil {
		return err
	}
	gauge.vec.WithLabelValues(labelValues...).Add(value)
	return nil
}

// SetValues / IncValues / DecValues / AddValues are the no-validation fast path for gauges:
// label values are passed positionally in registration order. See CounterMetric.IncValues.
func (gauge *GaugeMetric) SetValues(value float64, values ...string) {
	gauge.vec.WithLabelValues(values...).Set(value)
}

func (gauge *GaugeMetric) IncValues(values ...string) {
	gauge.vec.WithLabelValues(values...).Inc()
}

func (gauge *GaugeMetric) DecValues(values ...string) {
	gauge.vec.WithLabelValues(values...).Dec()
}

func (gauge *GaugeMetric) AddValues(value float64, values ...string) {
	gauge.vec.WithLabelValues(values...).Add(value)
}

// Bind resolves a fixed label set once and returns the concrete gauge. See CounterMetric.Bind.
func (gauge *GaugeMetric) Bind(values ...string) prometheus.Gauge {
	return gauge.vec.WithLabelValues(values...)
}

func (histogram *HistogramMetric) Observe(value float64, labels Labels) error {
	labelValues, err := orderedLabelValues(histogram.labelKeys, labels)
	if err != nil {
		return err
	}
	histogram.vec.WithLabelValues(labelValues...).Observe(value)
	return nil
}

func (histogram *HistogramMetric) ObserveDuration(
	duration time.Duration,
	labels Labels,
) error {
	return histogram.Observe(duration.Seconds(), labels)
}

// ObserveValues is the no-validation fast path for histograms: label values are passed
// positionally in registration order. See CounterMetric.IncValues.
func (histogram *HistogramMetric) ObserveValues(value float64, values ...string) {
	histogram.vec.WithLabelValues(values...).Observe(value)
}

// Bind resolves a fixed label set once and returns the concrete observer. See CounterMetric.Bind.
func (histogram *HistogramMetric) Bind(values ...string) prometheus.Observer {
	return histogram.vec.WithLabelValues(values...)
}

func orderedLabelValues(
	expectedLabelKeys []string,
	labels Labels,
) ([]string, error) {
	if len(labels) != len(expectedLabelKeys) {
		return nil, fmt.Errorf(
			"%w: expected labels %v, got %v",
			ErrInvalidLabelSet,
			expectedLabelKeys,
			keysOf(labels),
		)
	}

	values := make([]string, 0, len(expectedLabelKeys))
	for _, key := range expectedLabelKeys {
		value, exists := labels[key]
		if !exists {
			return nil, fmt.Errorf(
				"%w: missing label key %q, expected labels %v",
				ErrInvalidLabelSet,
				key,
				expectedLabelKeys,
			)
		}

		values = append(values, value)
	}

	return values, nil
}

func keysOf(labels Labels) []string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func cloneLabelKeys(labelKeys []string) []string {
	return append([]string(nil), labelKeys...)
}
