// Package scenario is the thin orchestrator every cmd/latency-* binary calls into: load config,
// provision accounts, run the spam pool around the caller's measurement loop, then print and
// persist the report. It sits above harness/spam/report/correlate so those packages stay
// decoupled from each other (spam depends on harness for account/environment types; putting this
// boilerplate in harness itself would need harness to import spam, an import cycle).
package scenario

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alex99y/matching-engine/loadtest/config"
	"github.com/alex99y/matching-engine/loadtest/internal/harness"
	"github.com/alex99y/matching-engine/loadtest/internal/report"
	"github.com/alex99y/matching-engine/loadtest/internal/spam"
)

// Deps is what a scenario Func needs to run its measurement loop.
type Deps struct {
	Env     *harness.Environment
	Cfg     *config.Config
	Spammer *spam.Spammer
}

// Func runs one test's measurement loop and returns the metrics it collected. ctx is cancelled
// if the process receives an interrupt; a well-behaved Func should stop sending and return.
type Func func(ctx context.Context, deps Deps) ([]report.Metric, error)

// Run is the entrypoint every cmd/latency-* main() calls. testName identifies the test in the
// report and names its account/output files.
func Run(testName string, fn Func) {
	cfg, err := config.NewConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("[%s] level %d (%s): provisioning %d maker(s) + %d taker(s) + 1 measured account on %s...\n",
		testName, cfg.Level, cfg.LevelName, cfg.MakerAccounts, cfg.TakerAccounts, cfg.Market)
	env, err := harness.Setup(ctx, cfg, testName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup error:", err)
		os.Exit(1)
	}

	spammer := spam.New(env, cfg.SpamRate)
	spammer.Start(ctx)

	fmt.Printf("[%s] warming up for %s (spam target %d op/s)...\n", testName, cfg.WarmupDuration, cfg.SpamRate)
	select {
	case <-time.After(cfg.WarmupDuration):
	case <-ctx.Done():
	}

	fmt.Printf("[%s] measuring for up to %s (%d samples)...\n", testName, cfg.Duration, cfg.SampleCount)
	startedAt := time.Now()
	metrics, runErr := fn(ctx, Deps{Env: env, Cfg: cfg, Spammer: spammer})
	duration := time.Since(startedAt)

	spammer.Stop()
	teardownCtx, teardownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer teardownCancel()
	if err := spammer.Cleanup(teardownCtx); err != nil {
		fmt.Fprintln(os.Stderr, "spam cleanup warning:", err)
	}
	if err := harness.CancelAllOpenOrders(teardownCtx, env.Client, env.Measured, cfg.Market); err != nil {
		fmt.Fprintln(os.Stderr, "measured-account cleanup warning:", err)
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "scenario error:", runErr)
		os.Exit(1)
	}

	stats := spammer.Stats()
	run := report.Run{
		TestName:         testName,
		Level:            cfg.Level,
		LevelName:        cfg.LevelName,
		Market:           cfg.Market,
		StartedAt:        startedAt,
		Duration:         duration,
		TargetSpamRate:   cfg.SpamRate,
		AchievedSpamRate: stats.AchievedRate,
		StreamReconnects: env.Stream.Reconnects(),
		Metrics:          metrics,
	}
	run.PrintSummary(os.Stdout)

	dir, err := run.Persist(cfg.OutputDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to persist report:", err)
		os.Exit(1)
	}
	fmt.Printf("[%s] results written to %s\n", testName, dir)
}
