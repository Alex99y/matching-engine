package client

import (
	"context"
	"net/http"
)

type FaucetCredit struct {
	Symbol string `json:"symbol"`
	Amount int64  `json:"amount"`
}

// RequestFunds credits the account behind token with the faucet's fixed amount of symbol.
// The faucet is uncapped and unthrottled, so callers loop this to build a large balance.
func (c *Client) RequestFunds(ctx context.Context, token, symbol string) (FaucetCredit, error) {
	var credit FaucetCredit
	err := c.do(ctx, http.MethodPost, "/faucet"+query(map[string]string{"instrument": symbol}),
		token, nil, &credit, http.StatusOK)
	return credit, err
}
