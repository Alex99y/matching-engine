package harness

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// MarketRules is a market's trading constraints plus the instrument decimals the REST
// market endpoint omits — enough to build valid orders and check settlement.
type MarketRules struct {
	Ref           string
	BaseSymbol    string
	QuoteSymbol   string
	BaseDecimals  int
	QuoteDecimals int
	BaseScale     uint64 // 10^BaseDecimals — the divisor in a notional (see core quoteAmount)
	PriceQuantum  uint64
	AmountQuantum uint64
	MinOrderSize  uint64
	MaxOrderSize  uint64
	TakerFeeBps   uint64
	MakerFeeBps   uint64
}

// ResolveMarket reads a market's rules and its base/quote instrument decimals.
func ResolveMarket(ctx context.Context, c *client.Client, ref string) (MarketRules, error) {
	m, err := c.GetMarket(ctx, ref)
	if err != nil {
		return MarketRules{}, fmt.Errorf("resolve market %s: %w", ref, err)
	}
	base, err := c.GetInstrument(ctx, m.BaseSymbol)
	if err != nil {
		return MarketRules{}, fmt.Errorf("resolve base instrument %s: %w", m.BaseSymbol, err)
	}
	quote, err := c.GetInstrument(ctx, m.QuoteSymbol)
	if err != nil {
		return MarketRules{}, fmt.Errorf("resolve quote instrument %s: %w", m.QuoteSymbol, err)
	}
	return MarketRules{
		Ref:           ref,
		BaseSymbol:    m.BaseSymbol,
		QuoteSymbol:   m.QuoteSymbol,
		BaseDecimals:  base.Decimals,
		QuoteDecimals: quote.Decimals,
		BaseScale:     pow10(base.Decimals),
		PriceQuantum:  m.PriceQuantum,
		AmountQuantum: m.AmountQuantum,
		MinOrderSize:  m.MinOrderSize,
		MaxOrderSize:  m.MaxOrderSize,
		TakerFeeBps:   m.TakerFeeBps,
		MakerFeeBps:   m.MakerFeeBps,
	}, nil
}

// Notional mirrors core's quoteAmount: price × qty ÷ BaseScale, in quote quanta.
func (r MarketRules) Notional(price, qty uint64) uint64 {
	return price * qty / r.BaseScale
}

func pow10(n int) uint64 {
	v := uint64(1)
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
