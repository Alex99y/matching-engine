package main

import (
	"github.com/alex99y/matching-engine/common/pkg/config"
	"github.com/alex99y/matching-engine/common/pkg/logger"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	logger := logger.NewLogger(logger.Info)

	postgresURL, err := config.GetPostgresURL()
	if err != nil {
		panic(err)
	}

	logger.Info("Migrating database")
	m, err := migrate.New("file://../migrations", postgresURL)
	if err != nil {
		panic(err)
	}
	if err := m.Up(); err != nil {
		panic(err)
	}
}
