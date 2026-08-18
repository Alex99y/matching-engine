package utils

import (
	"fmt"
	"strconv"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/config"
)

func StringEnv(key, def string) string {
	raw := config.GetConfigFromEnv(key)
	if raw == nil {
		return def
	}
	return *raw
}

func IntEnv(key string, def int) (int, error) {
	raw := config.GetConfigFromEnv(key)
	if raw == nil {
		return def, nil
	}
	v, err := strconv.Atoi(*raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
}

func DurationEnv(key string, def time.Duration) (time.Duration, error) {
	raw := config.GetConfigFromEnv(key)
	if raw == nil {
		return def, nil
	}
	v, err := time.ParseDuration(*raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
}
