package command

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/common/pkg/config"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var (
	instrumentRepo *repository.InstrumentRepository
	marketRepo     *repository.MarketRepository
	userRepo       *repository.UserRepository
)

var rootCmd = &cobra.Command{
	Use:          "cli",
	Short:        "Matching engine management CLI",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		postgresURL, err := config.GetPostgresURL()
		if err != nil {
			return err
		}

		log := logger.NewLogger(logger.Error)
		conn, err := postgres.Connect(context.Background(), postgresURL, postgres.DefaultConfig())
		if err != nil {
			return fmt.Errorf("database: %w", err)
		}

		instrumentRepo = repository.NewInstrumentRepository(log, conn)
		marketRepo = repository.NewMarketRepository(log, conn)
		userRepo = repository.NewUserRepository(log, conn)
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(newInstrumentCmd())
	rootCmd.AddCommand(newMarketCmd())
	rootCmd.AddCommand(newUserCmd())
}
