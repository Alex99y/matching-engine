package config_test

import (
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/config"
	"github.com/alex99y/matching-engine/common/pkg/logger"
)

func TestGetConfigFromEnv(t *testing.T) {
	t.Run("returns nil when unset", func(t *testing.T) {
		t.Setenv("ME_TEST_UNSET_VAR", "")
		if got := config.GetConfigFromEnv("ME_TEST_UNSET_VAR"); got != nil {
			t.Errorf("got %q, want nil", *got)
		}
	})

	t.Run("returns the value when set", func(t *testing.T) {
		t.Setenv("ME_TEST_SET_VAR", "hello")
		got := config.GetConfigFromEnv("ME_TEST_SET_VAR")
		if got == nil || *got != "hello" {
			t.Errorf("got %v, want \"hello\"", got)
		}
	})
}

func TestGetDebugLevel(t *testing.T) {
	t.Run("defaults to info when unset", func(t *testing.T) {
		t.Setenv(config.DebugLevel, "")
		if got := config.GetDebugLevel(); got != "info" {
			t.Errorf("got %q, want %q", got, "info")
		}
	})

	t.Run("returns the configured level", func(t *testing.T) {
		t.Setenv(config.DebugLevel, "debug")
		if got := config.GetDebugLevel(); got != "debug" {
			t.Errorf("got %q, want %q", got, "debug")
		}
	})
}

func TestGetMetricsPort(t *testing.T) {
	t.Run("errors when unset", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "")
		if _, err := config.GetMetricsPort(); err == nil {
			t.Error("expected an error when METRICS_PORT is unset")
		}
	})

	t.Run("errors on a non-integer value", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "not-a-number")
		if _, err := config.GetMetricsPort(); err == nil {
			t.Error("expected an error for a non-integer METRICS_PORT")
		}
	})

	t.Run("parses a valid value", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "9090")
		got, err := config.GetMetricsPort()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 9090 {
			t.Errorf("got %d, want 9090", got)
		}
	})
}

func TestGetPostgresURL(t *testing.T) {
	t.Run("errors when unset", func(t *testing.T) {
		t.Setenv(config.PostgresURL, "")
		if _, err := config.GetPostgresURL(); err == nil {
			t.Error("expected an error when POSTGRESQL_URL is unset")
		}
	})

	t.Run("returns the configured URL", func(t *testing.T) {
		t.Setenv(config.PostgresURL, "postgres://localhost/db")
		got, err := config.GetPostgresURL()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "postgres://localhost/db" {
			t.Errorf("got %q, want %q", got, "postgres://localhost/db")
		}
	})
}

func TestGetRabbitMQURL(t *testing.T) {
	t.Run("errors when unset", func(t *testing.T) {
		t.Setenv(config.RabbitMQURL, "")
		if _, err := config.GetRabbitMQURL(); err == nil {
			t.Error("expected an error when RABBITMQ_URL is unset")
		}
	})

	t.Run("returns the configured URL", func(t *testing.T) {
		t.Setenv(config.RabbitMQURL, "amqp://localhost")
		got, err := config.GetRabbitMQURL()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "amqp://localhost" {
			t.Errorf("got %q, want %q", got, "amqp://localhost")
		}
	})
}

func TestGetQueryTimeout(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		t.Setenv(config.QueryTimeout, "")
		got, err := config.GetQueryTimeout()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != config.DEFAULT_DB_TIMEOUT {
			t.Errorf("got %v, want %v", got, config.DEFAULT_DB_TIMEOUT)
		}
	})

	t.Run("parses a valid value as seconds", func(t *testing.T) {
		t.Setenv(config.QueryTimeout, "5")
		got, err := config.GetQueryTimeout()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 5*time.Second {
			t.Errorf("got %v, want %v", got, 5*time.Second)
		}
	})

	t.Run("errors on a non-integer value", func(t *testing.T) {
		t.Setenv(config.QueryTimeout, "2s")
		if _, err := config.GetQueryTimeout(); err == nil {
			t.Error("expected an error for a non-integer QUERY_TIMEOUT")
		}
	})

	// Locks in current behaviour, which is a hazard rather than a feature: a non-positive
	// duration makes every context.WithTimeout derived from it expire immediately, so every
	// query fails with DeadlineExceeded. Change these to expect an error if the getter is
	// made to reject <= 0.
	t.Run("accepts non-positive values", func(t *testing.T) {
		for _, v := range []string{"0", "-5"} {
			t.Setenv(config.QueryTimeout, v)
			got, err := config.GetQueryTimeout()
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", v, err)
			}
			if got > 0 {
				t.Errorf("%s: got %v, want a non-positive duration", v, got)
			}
		}
	})
}

func TestGetAllDefaultConfigs(t *testing.T) {
	t.Run("errors when metrics port missing", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "")
		t.Setenv(config.PostgresURL, "postgres://localhost/db")
		t.Setenv(config.RabbitMQURL, "amqp://localhost")
		if _, err := config.GetAllDefaultConfigs(); err == nil {
			t.Error("expected an error when METRICS_PORT is unset")
		}
	})

	t.Run("errors when postgres url missing", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "9090")
		t.Setenv(config.PostgresURL, "")
		t.Setenv(config.RabbitMQURL, "amqp://localhost")
		if _, err := config.GetAllDefaultConfigs(); err == nil {
			t.Error("expected an error when POSTGRESQL_URL is unset")
		}
	})

	t.Run("errors when rabbitmq url missing", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "9090")
		t.Setenv(config.PostgresURL, "postgres://localhost/db")
		t.Setenv(config.RabbitMQURL, "")
		if _, err := config.GetAllDefaultConfigs(); err == nil {
			t.Error("expected an error when RABBITMQ_URL is unset")
		}
	})

	t.Run("errors when query timeout is not an integer", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "9090")
		t.Setenv(config.PostgresURL, "postgres://localhost/db")
		t.Setenv(config.RabbitMQURL, "amqp://localhost")
		t.Setenv(config.QueryTimeout, "not-a-number")
		if _, err := config.GetAllDefaultConfigs(); err == nil {
			t.Error("expected an error when QUERY_TIMEOUT is not an integer")
		}
	})

	t.Run("builds a config from all env vars", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "9090")
		t.Setenv(config.PostgresURL, "postgres://localhost/db")
		t.Setenv(config.RabbitMQURL, "amqp://localhost")
		t.Setenv(config.DebugLevel, "debug")
		t.Setenv(config.QueryTimeout, "7")

		got, err := config.GetAllDefaultConfigs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := &config.Config{
			MetricsPort:    9090,
			PostgresURL:    "postgres://localhost/db",
			RabbitMQURL:    "amqp://localhost",
			DBQueryTimeout: 7 * time.Second,
			DebugLevel:     logger.Debug,
		}
		if *got != *want {
			t.Errorf("got %+v, want %+v", *got, *want)
		}
	})

	t.Run("falls back to the default query timeout when unset", func(t *testing.T) {
		t.Setenv(config.MetricsPort, "9090")
		t.Setenv(config.PostgresURL, "postgres://localhost/db")
		t.Setenv(config.RabbitMQURL, "amqp://localhost")
		t.Setenv(config.QueryTimeout, "")

		got, err := config.GetAllDefaultConfigs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.DBQueryTimeout != config.DEFAULT_DB_TIMEOUT {
			t.Errorf("got %v, want %v", got.DBQueryTimeout, config.DEFAULT_DB_TIMEOUT)
		}
	})
}
