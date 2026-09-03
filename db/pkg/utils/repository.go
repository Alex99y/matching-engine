package utils

import (
	"fmt"
	"time"
)

// minQueryTimeout is a floor, not a default. PostgreSQL's deadlock_timeout defaults to 1s, so a
// budget at or below it aborts a contended query before the deadlock detector ever runs, turning
// a diagnosable 40P01 into an opaque context deadline. It also rejects the zero value, which
// would otherwise make every derived context expire immediately.
const minQueryTimeout = time.Second

func ValidateTimeout(repository string, timeout time.Duration) {
	if timeout <= minQueryTimeout {
		panic(fmt.Sprintf(
			"%s: query timeout must be greater than %s, got %s",
			repository, minQueryTimeout, timeout,
		))
	}
}
