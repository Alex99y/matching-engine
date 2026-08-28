package client

import (
	"context"
	"net/http"
	"strconv"
)

// Valid candle intervals in seconds (mirrors the API's whitelist).
const (
	Interval1m  = 60
	Interval5m  = 300
	Interval15m = 900
	Interval1h  = 3600
	Interval4h  = 14400
	Interval1d  = 86400
)

// OHLCV values are decimal strings, like MarketPrice.
type Candle struct {
	BucketStart int64  `json:"bucket_start"`
	Open        string `json:"open"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Close       string `json:"close"`
	Volume      string `json:"volume"`
}

type Candles struct {
	Interval int64    `json:"interval"`
	Candles  []Candle `json:"candles"`
}

// GetCandles returns OHLCV buckets for [from, to] (unix seconds). The API caps the range at
// 1000 buckets and rejects an unknown interval with 400.
func (c *Client) GetCandles(ctx context.Context, marketRef string, intervalSec, from, to int64) (Candles, error) {
	q := map[string]string{
		"interval": strconv.FormatInt(intervalSec, 10),
		"from":     strconv.FormatInt(from, 10),
		"to":       strconv.FormatInt(to, 10),
	}
	var out Candles
	err := c.do(ctx, http.MethodGet, "/markets/"+marketRef+"/candles"+query(q), "", nil, &out, http.StatusOK)
	return out, err
}
