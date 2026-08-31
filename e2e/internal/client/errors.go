package client

import (
	"errors"
	"fmt"
)

// ErrUsernameTaken is returned by Register when the username already exists (409). Re-running
// the suite against a stack that already has the accounts is expected, not a failure.
var ErrUsernameTaken = errors.New("username already taken")

// HTTPError carries an unexpected API response. Tests assert on Status; Body holds the raw
// (usually {"message":"..."}) payload for debugging.
type HTTPError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: unexpected status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// Status returns the HTTP status carried by err, or 0 if err is not an *HTTPError. Lets a
// test write `client.Status(err) == 409` without an errors.As dance.
func Status(err error) int {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status
	}
	return 0
}

// OrderRejectedError is returned by CreateOrder/CancelOrder when the batch call itself
// succeeded (HTTP 2xx) but the single order in it was rejected per-item.
type OrderRejectedError struct {
	Reason string
}

func (e *OrderRejectedError) Error() string {
	return "order rejected: " + e.Reason
}
