package client

import (
	"context"
	"net/http"
)

type FaucetCredit struct {
	Symbol string `json:"symbol"`
	Amount int64  `json:"amount"`
}

func (c *Client) RequestFunds(ctx context.Context, token, instrumentSymbol string) (*FaucetCredit, error) {
	var credit FaucetCredit
	err := c.do(ctx, http.MethodPost, "/faucet?instrument="+instrumentSymbol, token, nil, &credit, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &credit, nil
}
