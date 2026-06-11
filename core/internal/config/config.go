package config

import (
	"strings"

	common "github.com/alex99y/matching-engine/common/pkg/config"
)

const (
	MarketList = "MARKET_LIST"
)

type CoreConfig struct {
	common.Config
	MarketList []string
}

func NewCoreConfig() *CoreConfig {
	defaultConfig, err := common.GetAllDefaultConfigs()
	if err != nil {
		panic(err)
	}

	markets := common.GetConfigFromEnv(MarketList)
	if markets == nil {
		panic("Market list cannot be empty, use the format: MARKET-1,MARKET-2,MARKET-3")
	}

	marketList := strings.Split((*markets), ",")

	return &CoreConfig{
		Config:     *defaultConfig,
		MarketList: marketList,
	}
}
