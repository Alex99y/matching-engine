// Package suite is the per-package TestMain glue for the e2e suites. A tests/ package does:
//
//	//go:build e2e
//	package orders
//
//	var env *suite.Env
//
//	func TestMain(m *testing.M) {
//		env = suite.Setup()
//		os.Exit(m.Run())
//	}
//
// Setup loads config, builds a client, waits for the stack, and resolves the market under
// test once.
package suite

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/e2e/internal/client"
	"github.com/alex99y/matching-engine/e2e/internal/config"
	"github.com/alex99y/matching-engine/e2e/internal/fixtures"
	"github.com/alex99y/matching-engine/e2e/internal/harness"
)

// Env is the shared context for every test in a package.
type Env struct {
	Cfg    *config.Config
	Client *client.Client
	Market harness.MarketRules // rules for Cfg.Market
}

// Setup builds the Env, or — on any failure (bad config, stack unreachable, market not
// seeded) — prints the reason and exits non-zero. An e2e run with no stack is a failure, not
// a skip: the caller opted in with `-tags e2e`. Call it from TestMain before m.Run().
func Setup() *Env {
	e, err := trySetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e/suite: %v\n", err)
		os.Exit(1)
	}
	return e
}

func trySetup() (*Env, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	c := client.New(cfg.APIURL)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ReadyTimeout+30*time.Second)
	defer cancel()

	if err := harness.WaitReady(ctx, c, cfg.ReadyTimeout); err != nil {
		return nil, err
	}
	if err := harness.RequireMarket(ctx, c, cfg.Market); err != nil {
		return nil, err
	}
	market, err := harness.ResolveMarket(ctx, c, cfg.Market)
	if err != nil {
		return nil, err
	}
	return &Env{Cfg: cfg, Client: c, Market: market}, nil
}

// Context returns the per-test context (deadline = cfg.SettleTimeout), cancelled via
// t.Cleanup. Use it for the test's API calls and for assert.Eventually / StreamStatus.
func (e *Env) Context(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), e.Cfg.SettleTimeout)
	t.Cleanup(cancel)
	return ctx
}

// NewAccount registers and logs in a fresh account, without funding it. Use it for tests
// that never place an order; NewFundedAccount otherwise.
func (e *Env) NewAccount(t *testing.T) *harness.Account {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	acc, err := harness.NewAccount(ctx, e.Client)
	if err != nil {
		t.Fatalf("suite: new account: %v", err)
	}
	return acc
}

// NewFundedAccount creates a fresh account, funds both legs of the market, and registers a
// t.Cleanup that sweeps any orders it leaves resting. It fails the test on any error.
func (e *Env) NewFundedAccount(t *testing.T) *harness.Account {
	t.Helper()

	setupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	acc := e.NewAccount(t)
	if err := acc.FundMarket(setupCtx, e.Client, e.Market, fixtures.FundingCalls); err != nil {
		t.Fatalf("suite: fund %s: %v", acc.Username, err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := harness.CancelAllOpen(ctx, e.Client, acc.LoginToken, e.Market.Ref); err != nil {
			t.Logf("suite: cleanup for %s: %v", acc.Username, err)
		}
	})
	return acc
}

// Admin returns the cli-backed admin helper, or skips the test when the binary is missing
// (the account freeze has no REST route — see PLAN.md §8).
func (e *Env) Admin(t *testing.T) *harness.Admin {
	t.Helper()

	admin, err := harness.NewAdmin(e.Cfg)
	if err != nil {
		t.Skipf("suite: admin actions unavailable: %v", err)
	}
	return admin
}
