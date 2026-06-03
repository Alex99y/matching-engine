package rabbitmq

import (
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/rabbitmq/amqp091-go"
)

type Config struct {
	ReconnectDelay time.Duration
	MaxReconnects  int // 0 = unlimited
}

func DefaultConfig() Config {
	return Config{
		ReconnectDelay: 5 * time.Second,
		MaxReconnects:  0,
	}
}

type RabbitMQClient struct {
	conn   *amqp091.Connection
	logger *logger.Logger
	uri    string
	cfg    Config
	mu     sync.RWMutex
	done   chan struct{}
}

func NewClient(log *logger.Logger, uri string, cfg Config) (*RabbitMQClient, error) {
	c := &RabbitMQClient{
		logger: log,
		uri:    uri,
		cfg:    cfg,
		done:   make(chan struct{}),
	}
	if err := c.connect(); err != nil {
		return nil, err
	}
	go c.watchConnection()
	return c, nil
}

func (c *RabbitMQClient) connect() error {
	conn, err := amqp091.Dial(c.uri)
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.logger.Info("Connected to RabbitMQ")
	return nil
}

// watchConnection listens for unexpected connection drops and triggers Reconnect.
// A clean Close() causes the NotifyClose channel to be closed (ok=false), which exits the loop.
func (c *RabbitMQClient) watchConnection() {
	for {
		c.mu.RLock()
		notifyClose := c.conn.NotifyClose(make(chan *amqp091.Error, 1))
		c.mu.RUnlock()

		select {
		case amqpErr, ok := <-notifyClose:
			if !ok {
				return
			}
			c.logger.Error(fmt.Sprintf("RabbitMQ connection lost: %v", amqpErr))
			c.Reconnect()
		case <-c.done:
			return
		}
	}
}

func (c *RabbitMQClient) Reconnect() {
	for attempts := 1; ; attempts++ {
		select {
		case <-c.done:
			return
		default:
		}

		if c.cfg.MaxReconnects > 0 && attempts > c.cfg.MaxReconnects {
			c.logger.Error("RabbitMQ: max reconnect attempts reached, giving up")
			return
		}

		c.logger.Info(fmt.Sprintf("RabbitMQ: reconnect attempt %d (delay %s)...", attempts, c.cfg.ReconnectDelay))

		select {
		case <-time.After(c.cfg.ReconnectDelay):
		case <-c.done:
			return
		}

		if err := c.connect(); err != nil {
			c.logger.Error(fmt.Sprintf("RabbitMQ: attempt %d failed: %v", attempts, err))
			continue
		}
		return
	}
}

func (c *RabbitMQClient) Channel() (*amqp091.Channel, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn == nil || c.conn.IsClosed() {
		return nil, fmt.Errorf("rabbitmq: connection unavailable")
	}
	return c.conn.Channel()
}

// CreateChannel opens a channel and applies QoS. prefetchCount limits the number
// of unacknowledged messages the broker will deliver; prefetchSize limits by bytes (0 = unlimited).
func (c *RabbitMQClient) CreateChannel(prefetchCount, prefetchSize int) (*amqp091.Channel, error) {
	ch, err := c.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Qos(prefetchCount, prefetchSize, false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("rabbitmq qos: %w", err)
	}
	return ch, nil
}

func (c *RabbitMQClient) Close() error {
	close(c.done)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn.Close()
	}
	return nil
}
