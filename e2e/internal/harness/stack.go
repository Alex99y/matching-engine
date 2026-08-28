// Package harness provisions what an e2e test needs against an already-running stack: it
// waits for the stack, resolves a market's trading rules, and creates funded throwaway
// accounts. It does not start or seed the stack — seeding must happen before the API boots
// (the API caches its market set at startup), so that is the job of `make stack-up` / the
// CI workflow, not this package.
package harness

import (
	"context"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// WaitReady blocks until the stack answers: GET /health is 200 and GET /markets succeeds
// (which also proves the api↔db path). It retries until timeout elapses or ctx is cancelled.
func WaitReady(ctx context.Context, c *client.Client, timeout time.Duration) error {
	const interval = 500 * time.Millisecond
	deadline := time.Now().Add(timeout)

	var lastErr error
	for {
		if lastErr = probe(ctx, c); lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("stack not ready after %s: %w", timeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func probe(ctx context.Context, c *client.Client) error {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.Health(pctx); err != nil {
		return fmt.Errorf("health: %w", err)
	}
	if _, err := c.ListMarkets(pctx); err != nil {
		return fmt.Errorf("list markets: %w", err)
	}
	return nil
}

// RequireMarket errors (with the fix in the message) if ref is not one the API serves.
func RequireMarket(ctx context.Context, c *client.Client, ref string) error {
	if _, err := c.GetMarket(ctx, ref); err != nil {
		return fmt.Errorf("market %q not available — seed instruments/markets before starting the API (see e2e/README.md): %w", ref, err)
	}
	return nil
}
