package command

import (
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
	if err := c.do(http.MethodPost, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *coreAdminClient) status() ([]marketStatus, error) {
	var out []marketStatus
	if err := c.do(http.MethodGet, "/admin/markets", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *coreAdminClient) do(method, path string, out any) error {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

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
		return fmt.Errorf("core admin api: %s: %s", resp.Status, apiMessage(body))
	}
	return json.Unmarshal(body, out)
}

// apiMessage pulls the message out of an error body, falling back to the raw body so a response
// from something that is not core (a proxy, the wrong port) is still legible.
func apiMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	return strings.TrimSpace(string(body))
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
