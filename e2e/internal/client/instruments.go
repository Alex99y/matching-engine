package client

import (
	"context"
	"net/http"
)

type Instrument struct {
	Name      string `json:"name"`
	Symbol    string `json:"symbol"`
	Decimals  int    `json:"decimals"`
	CreatedAt string `json:"created_at"` // RFC 3339
}

func (c *Client) ListInstruments(ctx context.Context) ([]Instrument, error) {
	var out []Instrument
	err := c.do(ctx, http.MethodGet, "/instruments/", "", nil, &out, http.StatusOK)
	return out, err
}

// GetInstrument returns one instrument by symbol. Unknown symbol → *HTTPError with Status 404.
func (c *Client) GetInstrument(ctx context.Context, symbol string) (Instrument, error) {
	var i Instrument
	err := c.do(ctx, http.MethodGet, "/instruments/"+symbol, "", nil, &i, http.StatusOK)
	return i, err
}
