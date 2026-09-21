package config

import (
	"fmt"

	common "github.com/alex99y/matching-engine/common/pkg/config"
	utils "github.com/alex99y/matching-engine/common/pkg/utils"
)

const (
	ServerPort               = "PORT"
	ServerHost               = "HOST"
	MaxPriceDeviationPercent = "MAX_PRICE_DEVIATION_PERCENT"

	defaultMaxPriceDeviationPercent = 10
)

type ApiConfig struct {
	common.Config
	ServerPort int
	ServerHost string
	// MaxPriceDeviationPercent is how far a limit price may sit from the market's last trade
	// before the order is refused. 0 disables the check.
	MaxPriceDeviationPercent uint
}

func NewApiConfig() *ApiConfig {
	defaultConfig, err := common.GetAllDefaultConfigs()
	if err != nil {
		panic(err)
	}

	var serverPortInt int
	serverPort := common.GetConfigFromEnv(ServerPort)
	if serverPort == nil {
		serverPortInt = 4000
	} else {
		serverPortInt, err = utils.StringToInt(*serverPort)
		if err != nil {
			panic(err)
		}
	}

	serverHost := common.GetConfigFromEnv(ServerHost)
	if serverHost == nil {
		defaultHost := "0.0.0.0"
		serverHost = &defaultHost
	}

	maxDeviation := defaultMaxPriceDeviationPercent
	if raw := common.GetConfigFromEnv(MaxPriceDeviationPercent); raw != nil {
		maxDeviation, err = utils.StringToInt(*raw)
		if err != nil {
			panic(err)
		}
		if maxDeviation < 0 {
			panic(fmt.Sprintf("%s must not be negative, got %d", MaxPriceDeviationPercent, maxDeviation))
		}
	}

	return &ApiConfig{
		Config:                   *defaultConfig,
		ServerPort:               serverPortInt,
		ServerHost:               *serverHost,
		MaxPriceDeviationPercent: uint(maxDeviation),
	}
}
