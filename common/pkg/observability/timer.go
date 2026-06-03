package observability

import "time"

type StopTimer func() time.Duration

// StartTimer captures the current time and returns a function
// that reports elapsed time since StartTimer was called.
func StartTimer() StopTimer {
	startedAt := time.Now()

	return func() time.Duration {
		return time.Since(startedAt)
	}
}
