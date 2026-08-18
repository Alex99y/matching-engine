package client

import (
	"context"
	"net/http"
)

type Market struct {
	BaseSymbol    string `json:"base_symbol"`
	QuoteSymbol   string `json:"quote_symbol"`
	PriceQuantum  uint64 `json:"price_quantum"`
	AmountQuantum uint64 `json:"amount_quantum"`
	MinOrderSize  uint64 `json:"min_order_size"`
	MaxOrderSize  uint64 `json:"max_order_size"`
}

// GetMarket fetches the market's trading rules so callers can construct valid prices/quantities
// (multiples of PriceQuantum/AmountQuantum, within [MinOrderSize, MaxOrderSize]) instead of
// hardcoding numbers that drift out of sync with seeded data.
func (c *Client) GetMarket(ctx context.Context, marketRef string) (*Market, error) {
	var m Market
	if err := c.do(ctx, http.MethodGet, "/markets/"+marketRef, "", nil, &m, http.StatusOK); err != nil {
		return nil, err
	}
	return &m, nil
}
