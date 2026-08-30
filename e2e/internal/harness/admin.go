package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/alex99y/matching-engine/e2e/internal/config"
)

// ErrNoCLI reports that the cli binary named by E2E_CLI_BIN does not exist, so admin
// actions are unavailable. A test that needs one should skip rather than fail — the binary
// is built by `make -C cli build` / `make stack-up`, not by the suite.
var ErrNoCLI = fmt.Errorf("cli binary not found")

// Admin runs the account actions the REST API does not expose (there is no freeze endpoint —
// see PLAN.md §8) by shelling out to the cli binary, which writes the database directly.
type Admin struct {
	bin         string
	postgresURL string
}

// NewAdmin returns nil, ErrNoCLI if the configured binary is missing.
func NewAdmin(cfg *config.Config) (*Admin, error) {
	if _, err := os.Stat(cfg.CLIBin); err != nil {
		return nil, fmt.Errorf("%w at %q (build it with `make -C cli build`): %v", ErrNoCLI, cfg.CLIBin, err)
	}
	return &Admin{bin: cfg.CLIBin, postgresURL: cfg.PostgresURL}, nil
}

// FreezeUser blocks the account's fund-moving routes (order creation, faucet). The flag is
// read from the users table on every authenticated request, so it takes effect immediately —
// existing sessions are not revoked.
func (a *Admin) FreezeUser(ctx context.Context, username string) error {
	return a.run(ctx, "user", "freeze", "--username", username)
}

func (a *Admin) UnfreezeUser(ctx context.Context, username string) error {
	return a.run(ctx, "user", "unfreeze", "--username", username)
}

func (a *Admin) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, a.bin, args...)
	cmd.Env = append(os.Environ(), "POSTGRESQL_URL="+a.postgresURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cli %v: %w: %s", args, err, out)
	}
	return nil
}
