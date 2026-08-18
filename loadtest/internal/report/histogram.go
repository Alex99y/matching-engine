package report

import (
	"encoding/json"
	"math"
	"sort"
	"time"
)

// Percentiles summarizes a set of latency samples. Zero-value (Count == 0) means no sample ever
// resolved — that is itself a meaningful, reportable outcome, not an error.
type Percentiles struct {
	Count int
	Min   time.Duration
	P50   time.Duration
	P90   time.Duration
	P95   time.Duration
	P99   time.Duration
	Max   time.Duration
}

type percentilesJSON struct {
	Count int     `json:"count"`
	MinMs float64 `json:"min_ms"`
	P50Ms float64 `json:"p50_ms"`
	P90Ms float64 `json:"p90_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
	MaxMs float64 `json:"max_ms"`
}

func (p Percentiles) MarshalJSON() ([]byte, error) {
	return json.Marshal(percentilesJSON{
		Count: p.Count,
		MinMs: msOf(p.Min),
		P50Ms: msOf(p.P50),
		P90Ms: msOf(p.P90),
		P95Ms: msOf(p.P95),
		P99Ms: msOf(p.P99),
		MaxMs: msOf(p.Max),
	})
}

func msOf(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func computePercentiles(samples []time.Duration) Percentiles {
	if len(samples) == 0 {
		return Percentiles{}
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	return Percentiles{
		Count: len(sorted),
		Min:   sorted[0],
		P50:   percentileOf(sorted, 50),
		P90:   percentileOf(sorted, 90),
		P95:   percentileOf(sorted, 95),
		P99:   percentileOf(sorted, 99),
		Max:   sorted[len(sorted)-1],
	}
}

// percentileOf uses the nearest-rank method on an already-sorted slice.
func percentileOf(sorted []time.Duration, p float64) time.Duration {
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
