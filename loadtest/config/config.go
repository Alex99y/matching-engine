package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	commonconfig "github.com/alex99y/matching-engine/common/pkg/config"
	"github.com/alex99y/matching-engine/loadtest/pkg/utils"
)

var (
	ErrMissingLevel = errors.New("LOADTEST_LEVEL is required")
	ErrInvalidLevel = errors.New("LOADTEST_LEVEL must be one of: 0, 1, 2, 3")
)

const envPrefix = "LOADTEST_"

const (
	envLevel         = envPrefix + "LEVEL"
	envAPIURL        = envPrefix + "API_URL"
	envMarket        = envPrefix + "MARKET"
	envDuration      = envPrefix + "DURATION"
	envSampleCount   = envPrefix + "SAMPLE_COUNT"
	envWarmup        = envPrefix + "WARMUP"
	envMakerAccounts = envPrefix + "MAKER_ACCOUNTS"
	envTakerAccounts = envPrefix + "TAKER_ACCOUNTS"
	envOutputDir     = envPrefix + "OUTPUT_DIR"
	envLogLevel      = envPrefix + "LOG_LEVEL"
)

const (
	defaultAPIURL        = "http://localhost:4000/api/v1"
	defaultMarket        = "ETH-USDT"
	defaultWarmup        = 5 * time.Second
	defaultMakerAccounts = 2
	defaultTakerAccounts = 2
	defaultOutputDir     = "./results"
	defaultLogLevel      = "info"
)

// LevelPreset bundles the spam rate (orders/sec the maker/taker pool sustains), total run
// duration, and measured-sample count that define one load level. Duration and SampleCount can
// each be overridden individually via LOADTEST_DURATION / LOADTEST_SAMPLE_COUNT without redefining the
// whole preset.
type LevelPreset struct {
	Name        string
	SpamRate    int
	Duration    time.Duration
	SampleCount int
}

// levelPresets are the pre-configured load environments (see project plan): level 0 is a
// no-spam baseline, level 3 sits just inside the matching engine's documented tuned throughput
// ceiling (~2,500 orders/s) so it stresses the engine without collapsing it outright.
var levelPresets = map[int]LevelPreset{
	0: {Name: "idle", SpamRate: 0, Duration: 30 * time.Second, SampleCount: 100},
	1: {Name: "low", SpamRate: 100, Duration: 60 * time.Second, SampleCount: 200},
	2: {Name: "medium", SpamRate: 600, Duration: 60 * time.Second, SampleCount: 200},
	3: {Name: "high", SpamRate: 1500, Duration: 90 * time.Second, SampleCount: 200},
}

type Config struct {
	Level       int
	LevelName   string
	SpamRate    int
	Duration    time.Duration
	SampleCount int

	APIURL         string
	Market         string
	WarmupDuration time.Duration
	MakerAccounts  int
	TakerAccounts  int
	OutputDir      string
	LogLevel       string
}

func NewConfig() (*Config, error) {
	rawLevel := commonconfig.GetConfigFromEnv(envLevel)
	if rawLevel == nil {
		return nil, ErrMissingLevel
	}
	level, err := strconv.Atoi(*rawLevel)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidLevel, err)
	}
	preset, ok := levelPresets[level]
	if !ok {
		return nil, ErrInvalidLevel
	}

	duration, err := utils.DurationEnv(envDuration, preset.Duration)
	if err != nil {
		return nil, err
	}
	if duration <= 0 {
		return nil, fmt.Errorf("%s must be a positive duration", envDuration)
	}

	sampleCount, err := utils.IntEnv(envSampleCount, preset.SampleCount)
	if err != nil {
		return nil, err
	}
	if sampleCount <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", envSampleCount)
	}

	warmup, err := utils.DurationEnv(envWarmup, defaultWarmup)
	if err != nil {
		return nil, err
	}

	makerAccounts, err := utils.IntEnv(envMakerAccounts, defaultMakerAccounts)
	if err != nil {
		return nil, err
	}
	if makerAccounts <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", envMakerAccounts)
	}

	takerAccounts, err := utils.IntEnv(envTakerAccounts, defaultTakerAccounts)
	if err != nil {
		return nil, err
	}
	if takerAccounts <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", envTakerAccounts)
	}

	return &Config{
		Level:       level,
		LevelName:   preset.Name,
		SpamRate:    preset.SpamRate,
		Duration:    duration,
		SampleCount: sampleCount,

		APIURL:         utils.StringEnv(envAPIURL, defaultAPIURL),
		Market:         utils.StringEnv(envMarket, defaultMarket),
		WarmupDuration: warmup,
		MakerAccounts:  makerAccounts,
		TakerAccounts:  takerAccounts,
		OutputDir:      utils.StringEnv(envOutputDir, defaultOutputDir),
		LogLevel:       utils.StringEnv(envLogLevel, defaultLogLevel),
	}, nil
}
