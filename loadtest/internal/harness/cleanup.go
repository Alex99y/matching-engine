package harness

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/loadtest/internal/client"
)

// CancelAllOpenOrders sweeps up every order acc still has resting in marketRef. Test runs
// deliberately leave orders open mid-run (that's the point of the cancel/match scenarios); this
// is teardown so a repeated invocation starts from a clean book instead of accumulating stale
// resting orders across runs.
func CancelAllOpenOrders(ctx context.Context, c *client.Client, acc *Account, marketRef string) error {
	ids, err := c.GetOpenOrderIDs(ctx, acc.Token, marketRef)
	if err != nil {
		return fmt.Errorf("list open orders for %s: %w", acc.Username, err)
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := c.CancelOrders(ctx, acc.Token, ids); err != nil {
		return fmt.Errorf("cancel open orders for %s: %w", acc.Username, err)
	}
	return nil
}
