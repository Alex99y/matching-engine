package stream

import (
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type candleClient struct {
	market   string
	interval int64 // seconds: 60, 300, 900, 3600, 14400, 86400
	ch       chan []byte
}

func (c *candleClient) channel() chan []byte { return c.ch }

// --- wire frames sent to candle SSE clients ---

type candleSnapshotMsg struct {
	Type        string `json:"type"` // "candle.snapshot"
	Interval    int64  `json:"interval"`
	BucketStart int64  `json:"bucket_start"`
	Open        string `json:"open"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Close       string `json:"close"`
	Volume      string `json:"volume"`
}

type candleTradeMsg struct {
	Type      string `json:"type"` // "candle.trade"
	Time      int64  `json:"time"` // unix seconds
	Price     string `json:"price"`
	Quantity  string `json:"quantity"`
	TakerSide string `json:"taker_side"`
}

type candleClosedMsg struct {
	Type        string `json:"type"` // "candle.closed"
	Interval    int64  `json:"interval"`
	BucketStart int64  `json:"bucket_start"`
}

func candleSnapshotFrame(interval, bucketStart int64, c *repository.Candle) []byte {
	msg := candleSnapshotMsg{
		Type:        "candle.snapshot",
		Interval:    interval,
		BucketStart: bucketStart,
		Open:        "0",
		High:        "0",
		Low:         "0",
		Close:       "0",
		Volume:      "0",
	}
	if c != nil {
		msg.Open = utils.FormatUint64(c.Open)
		msg.High = utils.FormatUint64(c.High)
		msg.Low = utils.FormatUint64(c.Low)
		msg.Close = utils.FormatUint64(c.Close)
		msg.Volume = utils.FormatUint64(c.Volume)
	}
	return marshalFrame(msg)
}

func candleTradeFrame(tradeSec int64, t marketdata.Trade) []byte {
	return marshalFrame(candleTradeMsg{
		Type:      "candle.trade",
		Time:      tradeSec,
		Price:     utils.FormatUint64(t.Price),
		Quantity:  utils.FormatUint64(t.Quantity),
		TakerSide: t.TakerSide,
	})
}

func candleClosedFrame(interval, bucketStart int64) []byte {
	return marshalFrame(candleClosedMsg{
		Type:        "candle.closed",
		Interval:    interval,
		BucketStart: bucketStart,
	})
}
