package client

import (
	"context"
	"net/http"
	"strconv"
)

type Balance struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Balance  int64  `json:"balance"` // available, in instrument quanta
	Blocked  int64  `json:"blocked"` // reserved against resting orders
	Frozen   int64  `json:"frozen"`  // admin-frozen portion
}

type Operation struct {
	ID        string `json:"id"`
	Symbol    string `json:"symbol"`
	Amount    int64  `json:"amount"`
	Type      string `json:"type"` // deposit | withdraw | freeze | unfreeze
	Reason    string `json:"reason,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

func (c *Client) GetBalances(ctx context.Context, token string) ([]Balance, error) {
	var out []Balance
	if err := c.do(ctx, http.MethodGet, "/users/balances", token, nil, &out, http.StatusOK); err != nil {
		return nil, err
	}
	return out, nil
}

// Balance returns the balance row for symbol, or a zero-value row if the user has none yet.
func (c *Client) Balance(ctx context.Context, token, symbol string) (Balance, error) {
	balances, err := c.GetBalances(ctx, token)
	if err != nil {
		return Balance{}, err
	}
	for _, b := range balances {
		if b.Symbol == symbol {
			return b, nil
		}
	}
	return Balance{Symbol: symbol}, nil
}

// GetOperations lists the user's ledger operations, newest first. limit <= 0 uses the API
// default.
func (c *Client) GetOperations(ctx context.Context, token string, limit int) ([]Operation, error) {
	q := map[string]string{}
	if limit > 0 {
		q["limit"] = strconv.Itoa(limit)
	}
	var out []Operation
	if err := c.do(ctx, http.MethodGet, "/users/operations"+query(q), token, nil, &out, http.StatusOK); err != nil {
		return nil, err
	}
	return out, nil
}
