package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

// deadLetter mirrors admin.DeadLetter on core's admin API.
type deadLetter struct {
	ID         int64           `json:"id"`
	MessageID  string          `json:"message_id,omitempty"`
	Market     string          `json:"market"`
	OrderID    string          `json:"order_id,omitempty"`
	EventType  string          `json:"event_type,omitempty"`
	Reason     string          `json:"reason"`
	Error      string          `json:"error,omitempty"`
	DeadAt     int64           `json:"dead_at"`
	RecordedAt int64           `json:"recorded_at"`
	Status     string          `json:"status"`
	Payload    json.RawMessage `json:"payload"`
}

type deadLetters struct {
	Items []deadLetter `json:"items"`
}

func (c *coreAdminClient) deadLetters(marketRef string, limit int) (*deadLetters, error) {
	q := url.Values{}
	if marketRef != "" {
		q.Set("market", marketRef)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/admin/dlq"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out deadLetters
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func newDlqCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dlq",
		Short: "Inspect the commands the matching engine could not process",
	}
	cmd.AddCommand(newDlqListCmd())
	return cmd
}

func newDlqListCmd() *cobra.Command {
	var coreURL, token, marketRef string
	var limit int
	var asJSON bool

	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List dead-lettered commands, newest first",
		Annotations: map[string]string{annotationSkipDB: "true"},
		Long: "Lists the commands core rejected — malformed, invalid against the market's rules,\n" +
			"of an unknown type, or poison (failed to commit deterministically) — as recorded in\n" +
			"the dead_letters table. Every market by default; --json includes the original payload.",
		Example: "  cli dlq list\n  cli dlq list --market ETH-USDT --limit 20\n  cli dlq list --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCoreAdminClient(coreURL, token)
			if err != nil {
				return err
			}
			result, err := client.deadLetters(strings.TrimSpace(marketRef), limit)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result.Items)
			}
			if len(result.Items) == 0 {
				fmt.Println("no dead letters")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tMARKET\tORDER\tTYPE\tREASON\tDEAD AT\tERROR")
			for _, d := range result.Items {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
					d.ID, d.Market, utils.DefaultIfBlank(d.OrderID, "-"), utils.DefaultIfBlank(d.EventType, "-"),
					d.Reason, time.Unix(d.DeadAt, 0).UTC().Format(time.RFC3339), utils.DefaultIfBlank(d.Error, "-"))
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&marketRef, "market", "", "only this market (format: BASE-QUOTE); default every market")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum rows (server default 50, cap 500)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the full records, including the original payload, as JSON")
	addCoreAdminFlags(cmd, &coreURL, &token)
	return cmd
}
