package config

import (
	common "github.com/alex99y/matching-engine/common/pkg/config"
	utils "github.com/alex99y/matching-engine/common/pkg/utils"
)

const (
	ServerPort = "PORT"
	ServerHost = "HOST"
)

type ApiConfig struct {
	common.Config
	ServerPort int
	ServerHost string
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

	return &ApiConfig{
		Config:     *defaultConfig,
		ServerPort: serverPortInt,
		ServerHost: *serverHost,
	}
}
