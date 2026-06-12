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

	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var (
	errMarketNameRequired             = errors.New("--name is required (format: BASE-QUOTE)")
	errMarketPriceQuantumNonPositive  = errors.New("--price_quantum must be > 0")
	errMarketAmountQuantumNonPositive = errors.New("--amount_quantum must be > 0")
	errMarketMinOrderSizeNonPositive  = errors.New("--min_order_size must be > 0")
	errMarketMaxOrderSizeNonPositive  = errors.New("--max_order_size must be > 0")
	errMarketMaxLtMin                 = errors.New("--max_order_size must be >= --min_order_size")
	errMarketFeeOutOfRange            = errors.New("--taker_fee_bps and --maker_fee_bps must be between 0 and 10000")
)

const maxFeeBps = 10000 // 100% in basis points

type marketInput struct {
	Name          string `json:"name"`
	PriceQuantum  int64  `json:"price_quantum"`
	AmountQuantum int64  `json:"amount_quantum"`
	MinOrderSize  int64  `json:"min_order_size"`
	MaxOrderSize  int64  `json:"max_order_size"`
	// Optional maker/taker fees in basis points; default 0 when omitted.
	TakerFeeBps int64 `json:"taker_fee_bps"`
	MakerFeeBps int64 `json:"maker_fee_bps"`
}

func (m *marketInput) normalize() {
	m.Name = strings.TrimSpace(m.Name)
}

func (m marketInput) validate() error {
	if m.Name == "" {
		return errMarketNameRequired
	}
	if _, _, err := utils.SplitMarketRef(m.Name); err != nil {
		return fmt.Errorf("invalid --name %q: must be BASE-QUOTE", m.Name)
	}
	if m.PriceQuantum <= 0 {
		return errMarketPriceQuantumNonPositive
	}
	if m.AmountQuantum <= 0 {
		return errMarketAmountQuantumNonPositive
	}
	if m.MinOrderSize <= 0 {
		return errMarketMinOrderSizeNonPositive
	}
	if m.MaxOrderSize <= 0 {
		return errMarketMaxOrderSizeNonPositive
	}
	if m.MaxOrderSize < m.MinOrderSize {
		return errMarketMaxLtMin
	}
	if m.MinOrderSize%m.AmountQuantum != 0 {
		return fmt.Errorf("--min_order_size (%d) must be a multiple of --amount_quantum (%d)", m.MinOrderSize, m.AmountQuantum)
	}
	if m.MaxOrderSize%m.AmountQuantum != 0 {
		return fmt.Errorf("--max_order_size (%d) must be a multiple of --amount_quantum (%d)", m.MaxOrderSize, m.AmountQuantum)
	}
	if m.TakerFeeBps < 0 || m.TakerFeeBps > maxFeeBps || m.MakerFeeBps < 0 || m.MakerFeeBps > maxFeeBps {
		return errMarketFeeOutOfRange
	}
	return nil
}

func newMarketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "market",
		Short: "Manage markets",
	}
	cmd.AddCommand(newMarketCreateCmd())
	cmd.AddCommand(newMarketGetCmd())
	return cmd
}

func newMarketCreateCmd() *cobra.Command {
	var (
		name          string
		priceQuantum  int64
		amountQuantum int64
		minOrderSize  int64
		maxOrderSize  int64
		takerFeeBps   int64
		makerFeeBps   int64
		jsonInput     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create one or more markets",
		Example: `  cli market create --name BTC-USDT --price_quantum 1 --amount_quantum 1000 --min_order_size 1000 --max_order_size 1000000000 --taker_fee_bps 100 --maker_fee_bps 50
  cli market create --json '[{"name":"BTC-USDT","price_quantum":1,"amount_quantum":1000,"min_order_size":1000,"max_order_size":1000000000,"taker_fee_bps":100,"maker_fee_bps":50}]'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			var inputs []marketInput
			if jsonInput != "" {
				if err := json.Unmarshal([]byte(jsonInput), &inputs); err != nil {
					return fmt.Errorf("invalid --json: %w", err)
				}
			} else {
				inputs = []marketInput{{
					Name:          name,
					PriceQuantum:  priceQuantum,
					AmountQuantum: amountQuantum,
					MinOrderSize:  minOrderSize,
					MaxOrderSize:  maxOrderSize,
					TakerFeeBps:   takerFeeBps,
					MakerFeeBps:   makerFeeBps,
				}}
			}

			var failed []string
			for _, inp := range inputs {
				inp.normalize()
				if err := inp.validate(); err != nil {
					failed = append(failed, fmt.Sprintf("market %q: %s", inp.Name, err))
					continue
				}

				baseSymbol, quoteSymbol, _ := utils.SplitMarketRef(inp.Name)
				baseSymbol = strings.ToUpper(baseSymbol)
				quoteSymbol = strings.ToUpper(quoteSymbol)

				err := marketRepo.CreateMarket(ctx, baseSymbol, quoteSymbol, inp.PriceQuantum, inp.AmountQuantum, inp.MinOrderSize, inp.MaxOrderSize, inp.TakerFeeBps, inp.MakerFeeBps)
				if err != nil {
					switch {
					case errors.Is(err, repository.ErrMarketAlreadyExists):
						failed = append(failed, fmt.Sprintf("market %q: already exists", inp.Name))
					case errors.Is(err, repository.ErrInvalidInstruments):
						failed = append(failed, fmt.Sprintf("market %q: instrument %s or %s not found — create them first", inp.Name, baseSymbol, quoteSymbol))
					default:
						failed = append(failed, fmt.Sprintf("market %q: %s", inp.Name, err))
					}
					continue
				}
				fmt.Printf("created market %s\n", inp.Name)
			}

			if len(failed) > 0 {
				return fmt.Errorf("%s", strings.Join(failed, "\n"))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "market name (format: BASE-QUOTE, e.g. BTC-USDT)")
	cmd.Flags().Int64Var(&priceQuantum, "price_quantum", 0, "minimum price increment (must be > 0)")
	cmd.Flags().Int64Var(&amountQuantum, "amount_quantum", 0, "minimum amount increment (must be > 0)")
	cmd.Flags().Int64Var(&minOrderSize, "min_order_size", 0, "minimum order size (must be a multiple of amount_quantum)")
	cmd.Flags().Int64Var(&maxOrderSize, "max_order_size", 0, "maximum order size (must be a multiple of amount_quantum, >= min_order_size)")
	cmd.Flags().Int64Var(&takerFeeBps, "taker_fee_bps", 0, "taker fee in basis points (0-10000, optional, default 0)")
	cmd.Flags().Int64Var(&makerFeeBps, "maker_fee_bps", 0, "maker fee in basis points (0-10000, optional, default 0)")
	cmd.Flags().StringVar(&jsonInput, "json", "", "JSON array of markets; overrides individual flags")

	return cmd
}

func newMarketGetCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "get",
		Short: "List markets, or fetch one by name",
		Example: `  cli market get
  cli market get --name BTC-USDT`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tBASE\tQUOTE\tPRICE_QUANTUM\tAMOUNT_QUANTUM\tMIN_ORDER_SIZE\tMAX_ORDER_SIZE\tTAKER_FEE_BPS\tMAKER_FEE_BPS")

			if name != "" {
				name = strings.TrimSpace(name)
				baseSymbol, quoteSymbol, err := utils.SplitMarketRef(name)
				if err != nil {
					return fmt.Errorf("invalid --name %q: must be BASE-QUOTE", name)
				}
				baseSymbol = strings.ToUpper(baseSymbol)
				quoteSymbol = strings.ToUpper(quoteSymbol)

				m, err := marketRepo.GetMarket(ctx, baseSymbol, quoteSymbol)
				if err != nil {
					if errors.Is(err, repository.ErrMarketNotFound) {
						return fmt.Errorf("market %q not found", name)
					}
					return err
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n", m.ID, m.BaseSymbol, m.QuoteSymbol, m.PriceQuantum, m.AmountQuantum, m.MinOrderSize, m.MaxOrderSize, m.TakerFeeBps, m.MakerFeeBps)
				return w.Flush()
			}

			markets, err := marketRepo.GetMarkets(ctx)
			if err != nil {
				return err
			}
			for _, m := range markets {
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n", m.ID, m.BaseSymbol, m.QuoteSymbol, m.PriceQuantum, m.AmountQuantum, m.MinOrderSize, m.MaxOrderSize, m.TakerFeeBps, m.MakerFeeBps)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "filter by market name (e.g. BTC-USDT)")
	return cmd
}
