package utils

import (
	"testing"
	"time"
)

func TestValidateTimeout(t *testing.T) {
	t.Run("accepts a timeout above the floor", func(t *testing.T) {
		ValidateTimeout("test repository", minQueryTimeout+time.Millisecond)
	})

	t.Run("panics at or below the floor", func(t *testing.T) {
		for _, timeout := range []time.Duration{
			0,
			-5 * time.Second,
			minQueryTimeout,
			minQueryTimeout - time.Millisecond,
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("expected a panic for timeout %s", timeout)
					}
				}()
				ValidateTimeout("test repository", timeout)
			}()
		}
	})
}
