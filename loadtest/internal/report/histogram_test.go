package report

import (
	"testing"
	"time"
)

func durations(ms ...int) []time.Duration {
	out := make([]time.Duration, len(ms))
	for i, m := range ms {
		out[i] = time.Duration(m) * time.Millisecond
	}
	return out
}

func TestComputePercentilesEmpty(t *testing.T) {
	p := computePercentiles(nil)
	if p.Count != 0 {
		t.Errorf("Count = %d, want 0", p.Count)
	}
}

func TestComputePercentilesSingleValue(t *testing.T) {
	p := computePercentiles(durations(10))
	if p.Count != 1 || p.Min != 10*time.Millisecond || p.P50 != 10*time.Millisecond || p.Max != 10*time.Millisecond {
		t.Errorf("got %+v, want every stat = 10ms, count 1", p)
	}
}

func TestComputePercentilesUnsortedInput(t *testing.T) {
	// 1..100ms, deliberately out of order — computePercentiles must sort before ranking.
	values := []int{}
	for i := 100; i >= 1; i-- {
		values = append(values, i)
	}
	p := computePercentiles(durations(values...))

	if p.Count != 100 {
		t.Fatalf("Count = %d, want 100", p.Count)
	}
	if p.Min != 1*time.Millisecond {
		t.Errorf("Min = %v, want 1ms", p.Min)
	}
	if p.Max != 100*time.Millisecond {
		t.Errorf("Max = %v, want 100ms", p.Max)
	}
	if p.P50 != 50*time.Millisecond {
		t.Errorf("P50 = %v, want 50ms", p.P50)
	}
	if p.P99 != 99*time.Millisecond {
		t.Errorf("P99 = %v, want 99ms", p.P99)
	}
}
