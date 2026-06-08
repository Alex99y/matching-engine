package observability

import (
	"testing"
	"time"
)

func TestStartTimerReturnsElapsedDuration(t *testing.T) {
	stopTimer := StartTimer()

	time.Sleep(10 * time.Millisecond)

	elapsed := stopTimer()
	if elapsed <= 0 {
		t.Fatalf("expected positive elapsed duration, got: %v", elapsed)
	}
}

func TestStartTimerElapsedIncreasesOverTime(t *testing.T) {
	stopTimer := StartTimer()

	time.Sleep(5 * time.Millisecond)
	first := stopTimer()

	time.Sleep(5 * time.Millisecond)
	second := stopTimer()

	if second <= first {
		t.Fatalf(
			"expected elapsed time to increase, first: %v second: %v",
			first,
			second,
		)
	}
}
