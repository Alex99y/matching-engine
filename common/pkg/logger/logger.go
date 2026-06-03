package logger

import (
	fmt "fmt"
	slog "log/slog"
	os "os"
)

type Logger struct {
	logger *slog.Logger
}

type DebugLevel string

const (
	Debug DebugLevel = "debug"
	Info  DebugLevel = "info"
	Warn  DebugLevel = "warn"
	Error DebugLevel = "error"
)

func DebugValueToLevel(lvl DebugLevel) slog.Level {
	switch lvl {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func NewLogger(lvl DebugLevel) *Logger {
	level := slog.Level(
		DebugValueToLevel(lvl),
	)
	return &Logger{
		logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})),
	}
}

func (l *Logger) Info(message string) {
	l.logger.Info(message)
}

func (l *Logger) Error(message string) {
	l.logger.Error(message)
}

func (l *Logger) Debug(message string) {
	l.logger.Debug(message)
}

func (l *Logger) Warn(message string) {
	l.logger.Warn(message)
}

func (l *Logger) ErrorO(err error) {
	l.logger.Error(fmt.Sprintf("%v", err))
}
