package main

import (
	"github.com/alex99y/matching-engine/common/pkg/logger"
	migrate "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	logger := logger.NewLogger(logger.Info)
	logger.Info("Migrating database")
	m, err := migrate.New(
		"file://../migrations",
		// commonConfig.PostgresURL,
		"",
	)
	if err != nil {
		panic(err)
	}
	if err := m.Up(); err != nil {
		panic(err)
	}
}
