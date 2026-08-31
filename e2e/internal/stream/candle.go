package stream

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Frame types on the candle stream: a snapshot of the bucket in progress on connect, one
// event per trade, and a marker when a bucket boundary is crossed.
const (
	EventCandleSnapshot = "candle.snapshot"
	EventCandleTrade    = "candle.trade"
	EventCandleClosed   = "candle.closed"
)

type CandleBucket struct {
	Interval    int64
	BucketStart int64
	Open        uint64
	High        uint64
	Low         uint64
	Close       uint64
	Volume      uint64
}

type CandleTrade struct {
	Time      int64 // unix seconds
	Price     uint64
	Quantity  uint64
	TakerSide string
}

// CandleEvent is the union of everything the candle stream carries. Exactly one of the
// pointers is set, matching Type; a "candle.closed" frame carries only Closed's boundary.
type CandleEvent struct {
	Type     string
	Snapshot *CandleBucket
	Trade    *CandleTrade
	Closed   *CandleBucket // Interval and BucketStart only
}

// CandleStream is a live subscriber to GET /stream/markets/{market}/candles.
type CandleStream struct{ *sub[CandleEvent] }

func ConnectCandles(ctx context.Context, apiURL, marketRef string, intervalSec int64) (*CandleStream, error) {
	url := strings.TrimRight(apiURL, "/") + "/stream/markets/" + marketRef +
		"/candles?interval=" + strconv.FormatInt(intervalSec, 10)

	s, err := subscribe(ctx, url, "", decodeCandle)
	if err != nil {
		return nil, err
	}
	return &CandleStream{s}, nil
}

func decodeCandle(raw []byte) (CandleEvent, bool, error) {
	kind, err := frameType(raw)
	if err != nil {
		return CandleEvent{}, false, err
	}

	switch kind {
	case EventCandleSnapshot:
		var w struct {
			Interval    int64  `json:"interval"`
			BucketStart int64  `json:"bucket_start"`
			Open        string `json:"open"`
			High        string `json:"high"`
			Low         string `json:"low"`
			Close       string `json:"close"`
			Volume      string `json:"volume"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return CandleEvent{}, false, err
		}
		return CandleEvent{Type: kind, Snapshot: &CandleBucket{
			Interval: w.Interval, BucketStart: w.BucketStart,
			Open: amount(w.Open), High: amount(w.High), Low: amount(w.Low),
			Close: amount(w.Close), Volume: amount(w.Volume),
		}}, true, nil

	case EventCandleTrade:
		var w struct {
			Time      int64  `json:"time"`
			Price     string `json:"price"`
			Quantity  string `json:"quantity"`
			TakerSide string `json:"taker_side"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return CandleEvent{}, false, err
		}
		return CandleEvent{Type: kind, Trade: &CandleTrade{
			Time: w.Time, Price: amount(w.Price), Quantity: amount(w.Quantity), TakerSide: w.TakerSide,
		}}, true, nil

	case EventCandleClosed:
		var w struct {
			Interval    int64 `json:"interval"`
			BucketStart int64 `json:"bucket_start"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return CandleEvent{}, false, err
		}
		return CandleEvent{Type: kind, Closed: &CandleBucket{
			Interval: w.Interval, BucketStart: w.BucketStart,
		}}, true, nil
	}
	return CandleEvent{}, false, nil
}

// WaitForSnapshot returns the opening snapshot of the bucket in progress.
func (s *CandleStream) WaitForSnapshot(ctx context.Context) (CandleBucket, error) {
	for {
		ev, err := s.Next(ctx)
		if err != nil {
			return CandleBucket{}, err
		}
		if ev.Type == EventCandleSnapshot {
			return *ev.Snapshot, nil
		}
	}
}

// WaitForTrade returns the next trade event at price.
func (s *CandleStream) WaitForTrade(ctx context.Context, price uint64) (CandleTrade, error) {
	for {
		ev, err := s.Next(ctx)
		if err != nil {
			return CandleTrade{}, err
		}
		if ev.Type == EventCandleTrade && ev.Trade.Price == price {
			return *ev.Trade, nil
		}
	}
}
