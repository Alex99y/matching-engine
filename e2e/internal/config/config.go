// Package config resolves the e2e suite's runtime settings from E2E_* environment
// variables, defaulting to an infra/local-deploy stack (see PLAN.md §5).
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	APIURL        string        // API base URL, including the /api/v1 prefix
	Market        string        // default market ref for single-market tests
	Markets       []string      // market-ref pool for sharded tests
	ReadyTimeout  time.Duration // how long to wait for the stack to accept traffic
	SettleTimeout time.Duration // cap for polling asynchronous post-order state
	LogLevel      string
}

const (
	defaultAPIURL        = "http://localhost:4000/api/v1"
	defaultMarket        = "ETH-USDT"
	defaultMarkets       = "ETH-USDT,BTC-USDT,ETH-BTC"
	defaultReadyTimeout  = 60 * time.Second
	defaultSettleTimeout = 5 * time.Second
	defaultLogLevel      = "info"
)

// Load returns an error only when a variable is set to an unparseable value; an unset
// variable takes its default.
func Load() (*Config, error) {
	ready, err := durationEnv("E2E_READY_TIMEOUT", defaultReadyTimeout)
	if err != nil {
		return nil, err
	}
	settle, err := durationEnv("E2E_SETTLE_TIMEOUT", defaultSettleTimeout)
	if err != nil {
		return nil, err
	}

	return &Config{
		APIURL:        strings.TrimRight(stringEnv("E2E_API_URL", defaultAPIURL), "/"),
		Market:        stringEnv("E2E_MARKET", defaultMarket),
		Markets:       splitCSV(stringEnv("E2E_MARKETS", defaultMarkets)),
		ReadyTimeout:  ready,
		SettleTimeout: settle,
		LogLevel:      stringEnv("E2E_LOG_LEVEL", defaultLogLevel),
	}, nil
}

func stringEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", key, raw, err)
	}
	return d, nil
}

func splitCSV(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
