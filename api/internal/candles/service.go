package candles

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var ErrFetchFailed = errors.New("failed to fetch candles")

type Candle struct {
	BucketStart int64
	Open        uint64
	High        uint64
	Low         uint64
	Close       uint64
	Volume      uint64
}

type CandleRepository interface {
	GetCandles(ctx context.Context, marketID int, intervalSec int64, from, to time.Time) ([]repository.Candle, error)
}

type CandleService struct {
	logger *logger.Logger
	repo   CandleRepository
}

func (s *CandleService) GetCandles(ctx context.Context, marketID int, intervalSec int64, from, to time.Time) ([]Candle, error) {
	rows, err := s.repo.GetCandles(ctx, marketID, intervalSec, from, to)
	if err != nil {
		return nil, ErrFetchFailed
	}
	out := make([]Candle, len(rows))
	for i, r := range rows {
		out[i] = Candle{
			BucketStart: r.BucketStart,
			Open:        r.Open,
			High:        r.High,
			Low:         r.Low,
			Close:       r.Close,
			Volume:      r.Volume,
		}
	}
	return out, nil
}

func NewCandleService(log *logger.Logger, repo CandleRepository) *CandleService {
	if log == nil {
		panic("logger cannot be nil")
	}
	if repo == nil {
		panic("repo cannot be nil")
	}
	return &CandleService{logger: log, repo: repo}
}
