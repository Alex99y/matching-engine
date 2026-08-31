package client

import (
	"context"
	"net/http"
	"strconv"
)

type MarketMatch struct {
	ID        string `json:"id"`
	Price     uint64 `json:"price"`
	Quantity  uint64 `json:"quantity"`
	TakerSide string `json:"taker_side"`
	MatchTime int64  `json:"match_time"`
}

// GetMatches returns the market's trade history, newest first. limit <= 0 uses the API
// default. Unknown market → *HTTPError with Status 404.
func (c *Client) GetMatches(ctx context.Context, marketRef string, limit int) ([]MarketMatch, error) {
	q := map[string]string{}
	if limit > 0 {
		q["limit"] = strconv.Itoa(limit)
	}
	var out []MarketMatch
	err := c.do(ctx, http.MethodGet, "/markets/"+marketRef+"/matches"+query(q), "", nil, &out, http.StatusOK)
	return out, err
}
