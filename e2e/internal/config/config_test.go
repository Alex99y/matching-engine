package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// t.Setenv on a key with no value still clears it for the test and restores after.
	for _, k := range []string{
		"E2E_API_URL", "E2E_MARKET", "E2E_MARKETS",
		"E2E_READY_TIMEOUT", "E2E_SETTLE_TIMEOUT", "E2E_LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIURL != defaultAPIURL || cfg.Market != defaultMarket {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if len(cfg.Markets) != 3 || cfg.Markets[0] != "ETH-USDT" {
		t.Fatalf("Markets = %v", cfg.Markets)
	}
	if cfg.ReadyTimeout != 60*time.Second || cfg.SettleTimeout != 15*time.Second {
		t.Fatalf("timeout defaults not applied: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("E2E_API_URL", "http://api.example:9000/api/v1/")
	t.Setenv("E2E_MARKET", "BTC-USDT")
	t.Setenv("E2E_MARKETS", " BTC-USDT , ETH-BTC ,")
	t.Setenv("E2E_READY_TIMEOUT", "90s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIURL != "http://api.example:9000/api/v1" {
		t.Fatalf("trailing slash not trimmed: %q", cfg.APIURL)
	}
	if cfg.Market != "BTC-USDT" {
		t.Fatalf("Market = %q", cfg.Market)
	}
	if len(cfg.Markets) != 2 || cfg.Markets[1] != "ETH-BTC" {
		t.Fatalf("Markets not split/trimmed: %v", cfg.Markets)
	}
	if cfg.ReadyTimeout != 90*time.Second {
		t.Fatalf("ReadyTimeout = %s", cfg.ReadyTimeout)
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	t.Setenv("E2E_SETTLE_TIMEOUT", "soon")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an unparseable duration")
	}
}
