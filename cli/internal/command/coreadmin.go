package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

// annotationSkipDB marks a command that reaches a service over HTTP rather than the database, so
// rootCmd's PersistentPreRunE skips connecting to Postgres for it.
const annotationSkipDB = "skipDB"

const (
	coreAdminURLEnv   = "CORE_ADMIN_URL"
	coreAdminTokenEnv = "ADMIN_TOKEN"
)

var (
	errAdminURLRequired   = errors.New("core admin URL is required: set " + coreAdminURLEnv + " or pass --core-url")
	errAdminTokenRequired = errors.New("admin token is required: set " + coreAdminTokenEnv + " or pass --token")
	errMarketRefRequired  = errors.New("--market is required (format: BASE-QUOTE)")

	errCancelTargetRequired  = errors.New("pass --all or at least one --order-id")
	errCancelTargetAmbiguous = errors.New("--all and --order-id are mutually exclusive")
)

// coreAdminClient talks to core's admin API — the per-market circuit breaker. Unlike every other
// command in this CLI, which writes the database directly, pausing a market is in-memory state
// owned by a running core process, so it can only be reached over HTTP.
type coreAdminClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type marketStatus struct {
	Market string `json:"market"`
	Paused bool   `json:"paused"`
}

func newCoreAdminClient(baseURL, token string) (*coreAdminClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(os.Getenv(coreAdminURLEnv), "/")
	}
	if baseURL == "" {
		return nil, errAdminURLRequired
	}

	token = strings.TrimSpace(token)
	if token == "" {
		token = os.Getenv(coreAdminTokenEnv)
	}
	if token == "" {
		return nil, errAdminTokenRequired
	}

	return &coreAdminClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (c *coreAdminClient) setPaused(marketRef string, paused bool) (*marketStatus, error) {
	action := "resume"
	if paused {
		action = "pause"
	}
	path := fmt.Sprintf("/admin/markets/%s/%s", url.PathEscape(marketRef), action)

	var out marketStatus
	if err := c.do(http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *coreAdminClient) status() ([]marketStatus, error) {
	var out []marketStatus
	if err := c.do(http.MethodGet, "/admin/markets", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *coreAdminClient) do(method, path string, in, out any) error {
	var payload io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, c.baseURL+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("core admin api at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core admin api: %s: %s", resp.Status, utils.APIErrorMessage(body))
	}
	return json.Unmarshal(body, out)
}

type userOrder struct {
	OrderID     string  `json:"order_id"`
	Market      string  `json:"market"`
	Side        string  `json:"side"`
	Price       *uint64 `json:"price"`
	Remaining   *uint64 `json:"remaining"`
	Type        string  `json:"type"`
	Open        bool    `json:"open"`
	Reachable   bool    `json:"reachable"`
	Unreachable string  `json:"unreachable_reason"`
}

type userOrders struct {
	Username string      `json:"username"`
	Frozen   bool        `json:"frozen"`
	Orders   []userOrder `json:"orders"`
}

type cancelResult struct {
	OrderID string `json:"order_id"`
	Market  string `json:"market"`
	Queued  bool   `json:"queued"`
	Reason  string `json:"reason"`
}

type cancelSummary struct {
	Username string         `json:"username"`
	Queued   int            `json:"queued"`
	Skipped  int            `json:"skipped"`
	Results  []cancelResult `json:"results"`
}

func (c *coreAdminClient) userOrders(username string, includeCancelled bool) (*userOrders, error) {
	path := "/admin/users/" + url.PathEscape(username) + "/orders"
	if includeCancelled {
		path += "?cancelled=true"
	}
	var out userOrders
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *coreAdminClient) cancelUserOrders(username string, orderIDs []string, all bool) (*cancelSummary, error) {
	body := map[string]any{"all": all}
	if len(orderIDs) > 0 {
		body["order_ids"] = orderIDs
	}
	var out cancelSummary
	if err := c.do(http.MethodPost, "/admin/users/"+url.PathEscape(username)+"/orders/cancel", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func newUserOrdersCmd() *cobra.Command {
	var username, coreURL, token string
	var includeCancelled bool

	cmd := &cobra.Command{
		Use:         "orders",
		Short:       "List a user's orders as core sees them",
		Annotations: map[string]string{annotationSkipDB: "true"},
		Long: "Lists the user's open orders, whether the account is frozen, and whether core can\n" +
			"actually cancel each one — an order on a paused or unserved market is reported as\n" +
			"unreachable rather than silently failing later.",
		Example: "  cli user orders --username alice\n  cli user orders --username alice --cancelled",
		RunE: func(cmd *cobra.Command, args []string) error {
			username = strings.TrimSpace(username)
			if username == "" {
				return errUserBalanceUsernameRequired
			}
			client, err := newCoreAdminClient(coreURL, token)
			if err != nil {
				return err
			}
			result, err := client.userOrders(username, includeCancelled)
			if err != nil {
				return err
			}

			fmt.Printf("user %s (frozen: %t)\n", result.Username, result.Frozen)
			if len(result.Orders) == 0 {
				fmt.Println("no orders")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ORDER ID\tMARKET\tSIDE\tPRICE\tREMAINING\tSTATE")
			for _, o := range result.Orders {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					o.OrderID, utils.DefaultIfBlank(o.Market, "-"), utils.DefaultIfBlank(o.Side, "-"), utils.FormatUint64PtrOr(o.Price, "-"), utils.FormatUint64PtrOr(o.Remaining, "-"), orderState(o))
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&username, "username", "", "username of the user")
	cmd.Flags().BoolVar(&includeCancelled, "cancelled", false, "include cancelled orders")
	addCoreAdminFlags(cmd, &coreURL, &token)
	return cmd
}

func newUserCancelOrdersCmd() *cobra.Command {
	var username, coreURL, token string
	var orderIDs []string
	var all bool

	cmd := &cobra.Command{
		Use:         "cancel-orders",
		Short:       "Cancel a frozen user's orders",
		Annotations: map[string]string{annotationSkipDB: "true"},
		Long: "Publishes a cancel for each targeted order, releasing the funds it had blocked.\n\n" +
			"The account must be frozen first (`cli user freeze`): that guards against wiping the\n" +
			"wrong user's book on a mistyped username, and stops them re-placing what you cancel.\n\n" +
			"Cancels are published, not applied — the matcher processes each in turn. Re-run\n" +
			"`cli user orders` to confirm the outcome.",
		Example: "  cli user cancel-orders --username alice --all\n" +
			"  cli user cancel-orders --username alice --order-id <uuid> --order-id <uuid>",
		RunE: func(cmd *cobra.Command, args []string) error {
			username = strings.TrimSpace(username)
			if username == "" {
				return errUserBalanceUsernameRequired
			}
			if !all && len(orderIDs) == 0 {
				return errCancelTargetRequired
			}
			if all && len(orderIDs) > 0 {
				return errCancelTargetAmbiguous
			}

			client, err := newCoreAdminClient(coreURL, token)
			if err != nil {
				return err
			}
			summary, err := client.cancelUserOrders(username, orderIDs, all)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ORDER ID\tMARKET\tRESULT")
			for _, r := range summary.Results {
				outcome := "queued"
				if !r.Queued {
					outcome = "SKIPPED: " + r.Reason
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", r.OrderID, utils.DefaultIfBlank(r.Market, "-"), outcome)
			}
			if err := w.Flush(); err != nil {
				return err
			}

			fmt.Printf("\n%d cancel(s) queued, %d skipped.\n", summary.Queued, summary.Skipped)
			if summary.Skipped > 0 {
				fmt.Println("Skipped orders are still live — resolve the reason above and re-run.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&username, "username", "", "username of the user")
	cmd.Flags().StringArrayVar(&orderIDs, "order-id", nil, "order id to cancel (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "cancel every open order the user has")
	addCoreAdminFlags(cmd, &coreURL, &token)
	return cmd
}

func orderState(o userOrder) string {
	switch {
	case !o.Open:
		return "closed"
	case o.Reachable:
		return "open"
	default:
		return "open (" + o.Unreachable + ")"
	}
}

func newMarketPauseCmd() *cobra.Command  { return newSetPausedCmd(true) }
func newMarketResumeCmd() *cobra.Command { return newSetPausedCmd(false) }

func newSetPausedCmd(paused bool) *cobra.Command {
	var market, coreURL, token string

	use, short := "resume", "Resume trading on a market that was paused"
	if paused {
		use, short = "pause", "Halt trading on a single market"
	}

	cmd := &cobra.Command{
		Use:         use,
		Short:       short,
		Annotations: map[string]string{annotationSkipDB: "true"},
		Long: pausedLong(paused) +
			"\n\nThe halt lives in the running core process, not the database: restarting core resumes\n" +
			"trading on every market.",
		Example: fmt.Sprintf("  cli market %s --market ETH-USDT", use),
		RunE: func(cmd *cobra.Command, args []string) error {
			market = strings.TrimSpace(market)
			if market == "" {
				return errMarketRefRequired
			}
			if _, _, err := utils.SplitMarketRef(market); err != nil {
				return fmt.Errorf("invalid market %q: %w", market, err)
			}

			client, err := newCoreAdminClient(coreURL, token)
			if err != nil {
				return err
			}
			status, err := client.setPaused(market, paused)
			if err != nil {
				return err
			}

			fmt.Printf("market %s %s\n", status.Market, pausedWord(status.Paused))
			return nil
		},
	}

	cmd.Flags().StringVar(&market, "market", "", "market ref to act on (e.g. ETH-USDT)")
	addCoreAdminFlags(cmd, &coreURL, &token)
	return cmd
}

func newMarketStatusCmd() *cobra.Command {
	var coreURL, token string

	cmd := &cobra.Command{
		Use:         "status",
		Short:       "Show which markets core is serving and whether each is paused",
		Annotations: map[string]string{annotationSkipDB: "true"},
		Example:     "  cli market status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCoreAdminClient(coreURL, token)
			if err != nil {
				return err
			}
			statuses, err := client.status()
			if err != nil {
				return err
			}
			if len(statuses) == 0 {
				fmt.Println("core is serving no markets")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "MARKET\tSTATE")
			for _, s := range statuses {
				fmt.Fprintf(w, "%s\t%s\n", s.Market, pausedWord(s.Paused))
			}
			return w.Flush()
		},
	}

	addCoreAdminFlags(cmd, &coreURL, &token)
	return cmd
}

func addCoreAdminFlags(cmd *cobra.Command, coreURL, token *string) {
	cmd.Flags().StringVar(coreURL, "core-url", "", "core admin API base URL (default $"+coreAdminURLEnv+")")
	cmd.Flags().StringVar(token, "token", "", "core admin API token (default $"+coreAdminTokenEnv+")")
}

func pausedWord(paused bool) string {
	if paused {
		return "paused"
	}
	return "trading"
}

func pausedLong(paused bool) string {
	if paused {
		return "Stops core consuming this market's command queue. Orders and cancels published while it\n" +
			"is paused accumulate in the broker and are processed on resume — nothing is rejected or\n" +
			"lost, and the order book is left untouched.\n\n" +
			"Note that cancels are held too, so resting orders cannot be withdrawn and their funds stay\n" +
			"blocked for the duration of the halt."
	}
	return "Restarts consumption of this market's command queue and drains whatever accumulated while\n" +
		"it was paused, in the order it arrived."
}
