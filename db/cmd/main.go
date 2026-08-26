package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

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

	m, err := migrate.New("file://../migrations", postgresURL)
	if err != nil {
		panic(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "down" {
		steps, err := parseSteps(os.Args[2:])
		if err != nil {
			panic(err)
		}
		logger.Info("Rolling back database")
		if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			panic(err)
		}
		return
	}

	logger.Info("Migrating database")
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		panic(err)
	}
}

func parseSteps(args []string) (int, error) {
	if len(args) == 0 {
		return 1, nil
	}
	steps, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("invalid steps argument %q: %w", args[0], err)
	}
	if steps <= 0 {
		return 0, fmt.Errorf("steps must be a positive integer, got %d", steps)
	}
	return steps, nil
}
