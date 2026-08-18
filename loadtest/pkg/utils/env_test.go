package utils_test

import (
	"testing"
	"time"

	"github.com/alex99y/matching-engine/loadtest/pkg/utils"
)

func TestStringEnv(t *testing.T) {
	t.Run("returns default when unset", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_STR", "")
		if got := utils.StringEnv("LOADTEST_TEST_STR", "fallback"); got != "fallback" {
			t.Errorf("got %q, want %q", got, "fallback")
		}
	})

	t.Run("returns the configured value", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_STR", "custom")
		if got := utils.StringEnv("LOADTEST_TEST_STR", "fallback"); got != "custom" {
			t.Errorf("got %q, want %q", got, "custom")
		}
	})
}

func TestIntEnv(t *testing.T) {
	t.Run("returns default when unset", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_INT", "")
		got, err := utils.IntEnv("LOADTEST_TEST_INT", 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 42 {
			t.Errorf("got %d, want 42", got)
		}
	})

	t.Run("returns the configured value", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_INT", "7")
		got, err := utils.IntEnv("LOADTEST_TEST_INT", 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 7 {
			t.Errorf("got %d, want 7", got)
		}
	})

	t.Run("errors on a non-integer value", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_INT", "not-a-number")
		if _, err := utils.IntEnv("LOADTEST_TEST_INT", 42); err == nil {
			t.Error("expected an error for a non-integer value")
		}
	})
}

func TestDurationEnv(t *testing.T) {
	t.Run("returns default when unset", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_DURATION", "")
		got, err := utils.DurationEnv("LOADTEST_TEST_DURATION", 5*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 5*time.Second {
			t.Errorf("got %v, want 5s", got)
		}
	})

	t.Run("returns the configured value", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_DURATION", "90s")
		got, err := utils.DurationEnv("LOADTEST_TEST_DURATION", 5*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 90*time.Second {
			t.Errorf("got %v, want 90s", got)
		}
	})

	t.Run("errors on an invalid value", func(t *testing.T) {
		t.Setenv("LOADTEST_TEST_DURATION", "not-a-duration")
		if _, err := utils.DurationEnv("LOADTEST_TEST_DURATION", 5*time.Second); err == nil {
			t.Error("expected an error for an invalid duration")
		}
	})
}
