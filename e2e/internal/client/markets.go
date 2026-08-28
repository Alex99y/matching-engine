package client

import (
	"context"
	"net/http"
	"strconv"
)

type MarketInfo struct {
	BaseSymbol    string `json:"base_symbol"`
	QuoteSymbol   string `json:"quote_symbol"`
	PriceQuantum  uint64 `json:"price_quantum"`
	AmountQuantum uint64 `json:"amount_quantum"`
	MinOrderSize  uint64 `json:"min_order_size"`
	MaxOrderSize  uint64 `json:"max_order_size"`
}

// Price fields are decimal strings (or null before the market has traded) — the API sends
// them formatted, not as raw quanta, unlike everything else.
type MarketPrice struct {
	Market           string  `json:"market"`
	Price            *string `json:"price"`
	MinPrice24h      *string `json:"min_price_24h"`
	MaxPrice24h      *string `json:"max_price_24h"`
	Volume24h        *string `json:"volume_24h"`
	ChangePercent24h *string `json:"change_percent_24h"`
}

type DepthLevel struct {
	Price    uint64 `json:"price"`
	Quantity uint64 `json:"quantity"`
}

type Depth struct {
	Market string       `json:"market"`
	Bids   []DepthLevel `json:"bids"` // high → low
	Asks   []DepthLevel `json:"asks"` // low → high
}

func (c *Client) ListMarkets(ctx context.Context) ([]MarketInfo, error) {
	var out []MarketInfo
	err := c.do(ctx, http.MethodGet, "/markets/", "", nil, &out, http.StatusOK)
	return out, err
}

// GetMarket returns a market's trading rules. Unknown market → *HTTPError with Status 404.
func (c *Client) GetMarket(ctx context.Context, marketRef string) (MarketInfo, error) {
	var m MarketInfo
	err := c.do(ctx, http.MethodGet, "/markets/"+marketRef, "", nil, &m, http.StatusOK)
	return m, err
}

func (c *Client) GetPrices(ctx context.Context) ([]MarketPrice, error) {
	var out []MarketPrice
	err := c.do(ctx, http.MethodGet, "/markets/prices", "", nil, &out, http.StatusOK)
	return out, err
}

// GetDepth reads the current order book. group is an optional price-bucket size (0 = raw).
func (c *Client) GetDepth(ctx context.Context, marketRef string, group uint64) (Depth, error) {
	q := map[string]string{}
	if group > 0 {
		q["group"] = strconv.FormatUint(group, 10)
	}
	var d Depth
	err := c.do(ctx, http.MethodGet, "/markets/"+marketRef+"/depth"+query(q), "", nil, &d, http.StatusOK)
	return d, err
}
