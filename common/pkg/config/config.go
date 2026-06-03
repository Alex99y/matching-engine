package config

import (
	"errors"
	"os"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

const RabbitMQURL = "RABBITMQ_URL"
const MetricsPort = "METRICS_PORT"
const PostgresURL = "POSTGRESQL_URL"

type Config struct {
	MetricsPort int
	PostgresURL string
	RabbitMQURL string
}

func GetEnvOrThrow(env string) *string {
	value := os.Getenv(env)
	if value == "" {
		return nil
	}
	return &value
}

func GetMetricsPort() (int, error) {
	metricsPort := GetEnvOrThrow(MetricsPort)
	if metricsPort == nil {
		return 0, errors.New("environment variable METRICS_PORT is not set")
	}

	metricsPortInt, err := utils.StringToInt(*metricsPort)
	if err != nil {
		return 0, errors.New("environment variable METRICS_PORT is not a valid integer")
	}

	return metricsPortInt, nil
}

func GetPostgresURL() (string, error) {
	postgresURL := GetEnvOrThrow(PostgresURL)
	if postgresURL == nil {
		return "", errors.New("environment variable POSTGRESQL_URL is not set")
	}
	return *postgresURL, nil
}

func GetRabbitMQURL() (string, error) {
	rabbitMQURL := GetEnvOrThrow(RabbitMQURL)
	if rabbitMQURL == nil {
		return "", errors.New("environment variable RABBITMQ_URL is not set")
	}
	return *rabbitMQURL, nil
}

func GetAllDefaultConfigs() (*Config, error) {

	metricsPort, err := GetMetricsPort()
	if err != nil {
		return nil, err
	}

	postgresURL, err := GetPostgresURL()
	if err != nil {
		return nil, err
	}

	rabbitMQURL, err := GetRabbitMQURL()
	if err != nil {
		return nil, err
	}

	return &Config{
		MetricsPort: metricsPort,
		PostgresURL: postgresURL,
		RabbitMQURL: rabbitMQURL,
	}, nil
}
