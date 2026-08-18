package config_test

import (
	"testing"
	"time"

	"github.com/alex99y/matching-engine/loadtest/config"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{
		"LOADTEST_LEVEL", "LOADTEST_API_URL", "LOADTEST_MARKET", "LOADTEST_DURATION", "LOADTEST_SAMPLE_COUNT",
		"LOADTEST_WARMUP", "LOADTEST_MAKER_ACCOUNTS", "LOADTEST_TAKER_ACCOUNTS", "LOADTEST_OUTPUT_DIR", "LOADTEST_LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
}

func TestNewConfigRequiresLevel(t *testing.T) {
	clearEnv(t)
	if _, err := config.NewConfig(); err == nil {
		t.Error("expected an error when LOADTEST_LEVEL is unset")
	}
}

func TestNewConfigRejectsUnknownLevel(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOADTEST_LEVEL", "7")
	if _, err := config.NewConfig(); err == nil {
		t.Error("expected an error for an out-of-range level")
	}
}

func TestNewConfigAppliesLevelPreset(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOADTEST_LEVEL", "2")
	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SpamRate != 600 {
		t.Errorf("SpamRate = %d, want 600", cfg.SpamRate)
	}
	if cfg.Duration != 60*time.Second {
		t.Errorf("Duration = %v, want 60s", cfg.Duration)
	}
	if cfg.SampleCount != 200 {
		t.Errorf("SampleCount = %d, want 200", cfg.SampleCount)
	}
	if cfg.LevelName != "medium" {
		t.Errorf("LevelName = %q, want %q", cfg.LevelName, "medium")
	}
}

func TestNewConfigLevelZeroDisablesSpam(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOADTEST_LEVEL", "0")
	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SpamRate != 0 {
		t.Errorf("SpamRate = %d, want 0", cfg.SpamRate)
	}
}

func TestNewConfigOverridesDurationAndSampleCount(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOADTEST_LEVEL", "1")
	t.Setenv("LOADTEST_DURATION", "5m")
	t.Setenv("LOADTEST_SAMPLE_COUNT", "42")

	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Duration != 5*time.Minute {
		t.Errorf("Duration = %v, want 5m", cfg.Duration)
	}
	if cfg.SampleCount != 42 {
		t.Errorf("SampleCount = %d, want 42", cfg.SampleCount)
	}
	// Overriding duration/samples must not disturb the preset's spam rate.
	if cfg.SpamRate != 100 {
		t.Errorf("SpamRate = %d, want 100", cfg.SpamRate)
	}
}

func TestNewConfigRejectsNonPositiveSampleCount(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOADTEST_LEVEL", "1")
	t.Setenv("LOADTEST_SAMPLE_COUNT", "0")
	if _, err := config.NewConfig(); err == nil {
		t.Error("expected an error for a zero sample count")
	}
}

func TestNewConfigDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOADTEST_LEVEL", "1")
	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIURL != "http://localhost:4000/api/v1" {
		t.Errorf("APIURL = %q, want default", cfg.APIURL)
	}
	if cfg.Market != "ETH-USDT" {
		t.Errorf("Market = %q, want default", cfg.Market)
	}
	if cfg.MakerAccounts != 2 || cfg.TakerAccounts != 2 {
		t.Errorf("MakerAccounts/TakerAccounts = %d/%d, want 2/2", cfg.MakerAccounts, cfg.TakerAccounts)
	}
	if cfg.WarmupDuration != 5*time.Second {
		t.Errorf("WarmupDuration = %v, want 5s", cfg.WarmupDuration)
	}
}
