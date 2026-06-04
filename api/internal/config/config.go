package config

import (
	"errors"

	common "github.com/alex99y/matching-engine/common/pkg/config"
	utils "github.com/alex99y/matching-engine/common/pkg/utils"
)

const (
	ServerPort   = "PORT"
	ServerHost   = "HOST"
	JWTSecretEnv = "JWT_SECRET"
)

type ApiConfig struct {
	common.Config
	ServerPort int
	ServerHost string
	JWTSecret  string
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

	jwtSecret := common.GetConfigFromEnv(JWTSecretEnv)
	if jwtSecret == nil {
		panic(errors.New("JWT secret is not set in environment variables"))
	}

	return &ApiConfig{
		Config:     *defaultConfig,
		ServerPort: serverPortInt,
		ServerHost: *serverHost,
		JWTSecret:  *jwtSecret,
	}
}
