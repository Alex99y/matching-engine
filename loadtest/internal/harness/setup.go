package harness

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/loadtest/config"
	"github.com/alex99y/matching-engine/loadtest/internal/client"
	"github.com/alex99y/matching-engine/loadtest/internal/stream"
)

// Environment is the fully-provisioned context a measurement scenario runs against: a funded
// maker/taker pool for spam (see internal/spam), a separate funded account whose orders are the
// ones actually measured, and that account's live order-event stream already connected.
type Environment struct {
	Client    *client.Client
	Market    *client.Market
	MarketRef string

	Makers   []*Account
	Takers   []*Account
	Measured *Account
	Stream   *stream.UserStream
}

// Setup provisions everything a test needs: resolves the market's trading rules, ensures and
// funds the maker/taker spam pool plus one measured account, and connects the measured account's
// private stream before returning — so by the time Setup returns, the caller can safely send
// orders it intends to measure without racing the listener's connection.
func Setup(ctx context.Context, cfg *config.Config, testName string) (*Environment, error) {
	c := client.New(cfg.APIURL)

	market, err := c.GetMarket(ctx, cfg.Market)
	if err != nil {
		return nil, fmt.Errorf("get market %s: %w", cfg.Market, err)
	}
	symbols := []string{market.BaseSymbol, market.QuoteSymbol}

	makers, err := provisionPool(ctx, c, cfg.MakerAccounts, MakerAccountName, symbols)
	if err != nil {
		return nil, fmt.Errorf("provision makers: %w", err)
	}
	takers, err := provisionPool(ctx, c, cfg.TakerAccounts, TakerAccountName, symbols)
	if err != nil {
		return nil, fmt.Errorf("provision takers: %w", err)
	}

	measured, err := EnsureAccount(ctx, c, MeasuredAccountName(testName))
	if err != nil {
		return nil, fmt.Errorf("provision measured account: %w", err)
	}
	if err := Fund(ctx, c, measured, symbols, defaultFundingCalls); err != nil {
		return nil, fmt.Errorf("fund measured account: %w", err)
	}

	userStream, err := stream.Connect(ctx, cfg.APIURL, measured.Token)
	if err != nil {
		return nil, fmt.Errorf("connect measured account's order stream: %w", err)
	}

	return &Environment{
		Client:    c,
		Market:    market,
		MarketRef: cfg.Market,
		Makers:    makers,
		Takers:    takers,
		Measured:  measured,
		Stream:    userStream,
	}, nil
}

func provisionPool(ctx context.Context, c *client.Client, count int, name func(int) string, symbols []string) ([]*Account, error) {
	pool := make([]*Account, count)
	for i := range pool {
		acc, err := EnsureAccount(ctx, c, name(i))
		if err != nil {
			return nil, err
		}
		if err := Fund(ctx, c, acc, symbols, defaultFundingCalls); err != nil {
			return nil, err
		}
		pool[i] = acc
	}
	return pool, nil
}
