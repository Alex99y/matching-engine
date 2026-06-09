package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var (
	errInstrumentNameRequired    = errors.New("--name is required")
	errInstrumentNameTooLong     = errors.New("--name must be at most 100 characters")
	errInstrumentSymbolRequired  = errors.New("--symbol is required")
	errInstrumentSymbolTooLong   = errors.New("--symbol must be at most 10 characters")
	errInstrumentDecimalsInvalid = errors.New("--decimals must be between 0 and 18")
)

type instrumentInput struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

func (i *instrumentInput) normalize() {
	i.Name = strings.TrimSpace(i.Name)
	i.Symbol = strings.ToUpper(strings.TrimSpace(i.Symbol))
}

func (i instrumentInput) validate() error {
	if i.Name == "" {
		return errInstrumentNameRequired
	}
	if len(i.Name) > 100 {
		return errInstrumentNameTooLong
	}
	if i.Symbol == "" {
		return errInstrumentSymbolRequired
	}
	if len(i.Symbol) > 10 {
		return errInstrumentSymbolTooLong
	}
	if i.Decimals < 0 || i.Decimals > 18 {
		return errInstrumentDecimalsInvalid
	}
	return nil
}

func newInstrumentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instrument",
		Short: "Manage instruments",
	}
	cmd.AddCommand(newInstrumentCreateCmd())
	cmd.AddCommand(newInstrumentGetCmd())
	return cmd
}

func newInstrumentCreateCmd() *cobra.Command {
	var (
		name      string
		symbol    string
		decimals  int
		jsonInput string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create one or more instruments",
		Example: `  cli instrument create --name Bitcoin --symbol BTC --decimals 8
  cli instrument create --json '[{"name":"Bitcoin","symbol":"BTC","decimals":8},{"name":"Tether","symbol":"USDT","decimals":6}]'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			var inputs []instrumentInput
			if jsonInput != "" {
				if err := json.Unmarshal([]byte(jsonInput), &inputs); err != nil {
					return fmt.Errorf("invalid --json: %w", err)
				}
			} else {
				inputs = []instrumentInput{{Name: name, Symbol: symbol, Decimals: decimals}}
			}

			var failed []string
			for _, inp := range inputs {
				inp.normalize()
				if err := inp.validate(); err != nil {
					failed = append(failed, fmt.Sprintf("instrument %q: %s", inp.Symbol, err))
					continue
				}
				if err := instrumentRepo.CreateNewInstrument(ctx, inp.Name, inp.Symbol, inp.Decimals); err != nil {
					if errors.Is(err, repository.ErrInstrumentAlreadyExists) {
						failed = append(failed, fmt.Sprintf("instrument %q: already exists", inp.Symbol))
						continue
					}
					failed = append(failed, fmt.Sprintf("instrument %q: %s", inp.Symbol, err))
					continue
				}
				fmt.Printf("created instrument %s\n", inp.Symbol)
			}

			if len(failed) > 0 {
				return fmt.Errorf("%s", strings.Join(failed, "\n"))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "instrument full name (e.g. Bitcoin)")
	cmd.Flags().StringVar(&symbol, "symbol", "", "ticker symbol, max 10 chars (e.g. BTC)")
	cmd.Flags().IntVar(&decimals, "decimals", 0, "decimal precision, 0–18")
	cmd.Flags().StringVar(&jsonInput, "json", "", "JSON array of instruments; overrides individual flags")

	return cmd
}

func newInstrumentGetCmd() *cobra.Command {
	var symbol string

	cmd := &cobra.Command{
		Use:   "get",
		Short: "List instruments, or fetch one by symbol",
		Example: `  cli instrument get
  cli instrument get --symbol BTC`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSYMBOL\tDECIMALS\tCREATED_AT")

			if symbol != "" {
				symbol = strings.ToUpper(strings.TrimSpace(symbol))
				i, err := instrumentRepo.GetInstrument(ctx, symbol)
				if err != nil {
					if errors.Is(err, repository.ErrInstrumentNotFound) {
						return fmt.Errorf("instrument %q not found", symbol)
					}
					return err
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", i.ID, i.Name, i.Symbol, i.Decimals, i.CreatedAt.Format("2006-01-02 15:04:05"))
				return w.Flush()
			}

			instruments, err := instrumentRepo.GetInstruments(ctx)
			if err != nil {
				return err
			}
			for _, i := range instruments {
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", i.ID, i.Name, i.Symbol, i.Decimals, i.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&symbol, "symbol", "", "filter by symbol")
	return cmd
}
