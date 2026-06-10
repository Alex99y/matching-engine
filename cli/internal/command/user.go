package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var (
	errUserBalanceUsernameRequired   = errors.New("--username is required")
	errUserBalanceInstrumentRequired = errors.New("--instrument is required")
	errUserBalanceAmountNonPositive  = errors.New("--amount must be > 0")
)

func newUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
	}
	cmd.AddCommand(newUserBalanceCmd())
	return cmd
}

func newUserBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "Manage user balances",
	}
	cmd.AddCommand(newUserBalanceAddCmd())
	cmd.AddCommand(newUserBalanceRemoveCmd())
	return cmd
}

func newUserBalanceAddCmd() *cobra.Command {
	var (
		username   string
		instrument string
		amount     int64
	)

	cmd := &cobra.Command{
		Use:     "add",
		Short:   "Add balance to a user for an instrument",
		Example: `  cli user balance add --instrument BTC --username alice --amount 100000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			username = strings.TrimSpace(username)
			instrument = strings.ToUpper(strings.TrimSpace(instrument))

			if username == "" {
				return errUserBalanceUsernameRequired
			}
			if instrument == "" {
				return errUserBalanceInstrumentRequired
			}
			if amount <= 0 {
				return errUserBalanceAmountNonPositive
			}

			ctx := context.Background()

			user, err := userRepo.GetUserByUsername(ctx, username)
			if err != nil {
				if errors.Is(err, repository.ErrUserNotFound) {
					return fmt.Errorf("user %q not found", username)
				}
				return err
			}

			instr, err := instrumentRepo.GetInstrument(ctx, instrument)
			if err != nil {
				if errors.Is(err, repository.ErrInstrumentNotFound) {
					return fmt.Errorf("instrument %q not found", instrument)
				}
				return err
			}

			if err := userRepo.AddUserBalance(ctx, user.ID, instr.ID, amount); err != nil {
				return err
			}

			fmt.Printf("added %d to %s balance for user %s\n", amount, instrument, username)
			return nil
		},
	}

	cmd.Flags().StringVar(&username, "username", "", "username of the user")
	cmd.Flags().StringVar(&instrument, "instrument", "", "instrument symbol (e.g. BTC)")
	cmd.Flags().Int64Var(&amount, "amount", 0, "amount to add (must be > 0)")

	return cmd
}

func newUserBalanceRemoveCmd() *cobra.Command {
	var (
		username   string
		instrument string
		amount     int64
	)

	cmd := &cobra.Command{
		Use:     "remove",
		Short:   "Remove balance from a user for an instrument",
		Example: `  cli user balance remove --instrument BTC --username alice --amount 100000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			username = strings.TrimSpace(username)
			instrument = strings.ToUpper(strings.TrimSpace(instrument))

			if username == "" {
				return errUserBalanceUsernameRequired
			}
			if instrument == "" {
				return errUserBalanceInstrumentRequired
			}
			if amount <= 0 {
				return errUserBalanceAmountNonPositive
			}

			ctx := context.Background()

			user, err := userRepo.GetUserByUsername(ctx, username)
			if err != nil {
				if errors.Is(err, repository.ErrUserNotFound) {
					return fmt.Errorf("user %q not found", username)
				}
				return err
			}

			instr, err := instrumentRepo.GetInstrument(ctx, instrument)
			if err != nil {
				if errors.Is(err, repository.ErrInstrumentNotFound) {
					return fmt.Errorf("instrument %q not found", instrument)
				}
				return err
			}

			if err := userRepo.RemoveUserBalance(ctx, user.ID, instr.ID, amount); err != nil {
				if errors.Is(err, repository.ErrInsufficientBalance) {
					return fmt.Errorf("user %q does not have sufficient %s balance", username, instrument)
				}
				return err
			}

			fmt.Printf("removed %d from %s balance for user %s\n", amount, instrument, username)
			return nil
		},
	}

	cmd.Flags().StringVar(&username, "username", "", "username of the user")
	cmd.Flags().StringVar(&instrument, "instrument", "", "instrument symbol (e.g. BTC)")
	cmd.Flags().Int64Var(&amount, "amount", 0, "amount to remove (must be > 0)")

	return cmd
}
