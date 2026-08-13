package logger_test

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
)

func TestDebugValueToLevel(t *testing.T) {
	cases := []struct {
		lvl  logger.DebugLevel
		want slog.Level
	}{
		{logger.Debug, slog.LevelDebug},
		{logger.Info, slog.LevelInfo},
		{logger.Warn, slog.LevelWarn},
		{logger.Error, slog.LevelError},
		{logger.DebugLevel("unknown"), slog.LevelInfo}, // unrecognized falls back to info
	}
	for _, c := range cases {
		if got := logger.DebugValueToLevel(c.lvl); got != c.want {
			t.Errorf("DebugValueToLevel(%q) = %v, want %v", c.lvl, got, c.want)
		}
	}
}

// captureStdout swaps os.Stdout for a pipe before running fn, so anything written
// through a *Logger constructed inside fn can be inspected afterward. The swap must
// happen before NewLogger is called: NewLogger binds the *os.File value current at
// construction time, not a live reference to the os.Stdout variable.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

func TestLoggerWritesAtOrAboveConfiguredLevel(t *testing.T) {
	output := captureStdout(t, func() {
		log := logger.NewLogger(logger.Warn)
		log.Info("should be suppressed")
		log.Warn("should appear")
	})

	if strings.Contains(output, "should be suppressed") {
		t.Errorf("expected Info to be suppressed at Warn level, got: %s", output)
	}
	if !strings.Contains(output, "should appear") {
		t.Errorf("expected Warn message in output, got: %s", output)
	}
}

func TestLoggerEveryLevel(t *testing.T) {
	output := captureStdout(t, func() {
		log := logger.NewLogger(logger.Debug)
		log.Debug("debug msg")
		log.Info("info msg")
		log.Warn("warn msg")
		log.Error("error msg")
	})

	for _, want := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got: %s", want, output)
		}
	}
}

func TestLoggerErrorO(t *testing.T) {
	output := captureStdout(t, func() {
		log := logger.NewLogger(logger.Error)
		log.ErrorO(errors.New("boom"))
	})

	if !strings.Contains(output, "boom") {
		t.Errorf("expected error text in output, got: %s", output)
	}
}
