package harness

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// CancelAllOpen cancels every order the account currently has resting in marketRef. Best
// effort: it is meant for test teardown, so an order that filled or was already cancelled
// between the list and the cancel is not an error.
func CancelAllOpen(ctx context.Context, c *client.Client, token, marketRef string) error {
	orders, err := c.ListOrders(ctx, token, client.OrdersFilter{Market: marketRef, ShowOpen: true})
	if err != nil {
		return fmt.Errorf("list open orders: %w", err)
	}

	ids := make([]string, 0, len(orders))
	for _, o := range orders {
		if o.OpenOrder != nil {
			ids = append(ids, o.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := c.CancelOrders(ctx, token, ids); err != nil {
		return fmt.Errorf("cancel %d open orders: %w", len(ids), err)
	}
	return nil
}
