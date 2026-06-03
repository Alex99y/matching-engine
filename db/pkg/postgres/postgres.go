package postgres

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/lib/pq"
)

type Config struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxOpenConns:    25,
		MaxIdleConns:    25,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

func Connect(ctx context.Context, url string, cfg Config) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
