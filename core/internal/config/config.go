package config

import (
	"strings"

	common "github.com/alex99y/matching-engine/common/pkg/config"
	"github.com/alex99y/matching-engine/common/pkg/utils"
)

const (
	MarketList = "MARKET_LIST"
	AdminPort  = "ADMIN_PORT"
	AdminToken = "ADMIN_TOKEN"
)

type CoreConfig struct {
	common.Config
	MarketList []string
	AdminPort  int
	AdminToken string
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

	adminPort := common.GetConfigFromEnv(AdminPort)
	if adminPort == nil {
		panic("ADMIN_PORT is required — the admin API serves the per-market circuit breaker")
	}
	adminPortInt, err := utils.StringToInt(*adminPort)
	if err != nil {
		panic("ADMIN_PORT is not a valid integer")
	}

	// Admin token required to communicate with the core via API
	adminToken := common.GetConfigFromEnv(AdminToken)
	if adminToken == nil {
		panic("ADMIN_TOKEN is required; it authenticates the admin API")
	}

	return &CoreConfig{
		Config:     *defaultConfig,
		MarketList: marketList,
		AdminPort:  adminPortInt,
		AdminToken: *adminToken,
	}
}
