// client_test.go covers only the connection-independent parts of RabbitMQClient.
// Everything else in this package (connect, Reconnect, watchConnection, Channel,
// CreateChannel, Close, NewQueue, Exchange/Subscriber declare/publish/consume) drives
// a real *amqp091.Connection/*amqp091.Channel and needs a live broker — consistent
// with the rest of this codebase, that I/O-bound behavior isn't unit tested here
// (e.g. db/pkg/postgres's connection code has no unit tests either).
package rabbitmq

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ReconnectDelay != 5*time.Second {
		t.Errorf("ReconnectDelay = %v, want 5s", cfg.ReconnectDelay)
	}
	if cfg.MaxReconnects != 0 {
		t.Errorf("MaxReconnects = %d, want 0 (unlimited)", cfg.MaxReconnects)
	}
}

func TestRabbitMQClientString(t *testing.T) {
	c := &RabbitMQClient{uri: "amqp://user:secret@localhost:5672/"}

	got := c.String()
	if got != "RabbitMQClient{uri: [redacted]}" {
		t.Errorf("String() = %q, want the redacted representation", got)
	}
	if strings.Contains(got, "secret") {
		t.Error("String() must never leak the connection URI")
	}
}
