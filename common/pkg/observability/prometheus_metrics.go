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
