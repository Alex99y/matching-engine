package command

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/common/pkg/config"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type instrumentRepository interface {
	CreateNewInstrument(ctx context.Context, name, symbol string, decimals int) error
	GetInstrument(ctx context.Context, symbol string) (*repository.Instrument, error)
	GetInstruments(ctx context.Context) ([]repository.Instrument, error)
}

type marketRepository interface {
	CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize, takerFeeBps, makerFeeBps int64) error
	GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error)
	GetMarkets(ctx context.Context) ([]repository.Market, error)
}

type userRepository interface {
	GetUserByUsername(ctx context.Context, username string) (*repository.User, error)
	FreezeUser(ctx context.Context, userID uuid.UUID) error
	UnfreezeUser(ctx context.Context, userID uuid.UUID) error
	AddUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error
	RemoveUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error
	FreezeUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error
	UnfreezeUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error
}

var (
	instrumentRepo instrumentRepository
	marketRepo     marketRepository
	userRepo       userRepository
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
