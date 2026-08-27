package candles_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/candles"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type fakeCandleRepository struct {
	rows []repository.Candle
	err  error

	gotMarketID    int
	gotIntervalSec int64
	gotFrom, gotTo time.Time
}

func (f *fakeCandleRepository) GetCandles(ctx context.Context, marketID int, intervalSec int64, from, to time.Time) ([]repository.Candle, error) {
	f.gotMarketID = marketID
	f.gotIntervalSec = intervalSec
	f.gotFrom = from
	f.gotTo = to
	return f.rows, f.err
}

func newTestService(repo candles.CandleRepository) *candles.CandleService {
	return candles.NewCandleService(logger.NewLogger(logger.Error), repo)
}

func TestGetCandlesPassesArgsThrough(t *testing.T) {
	repo := &fakeCandleRepository{}
	svc := newTestService(repo)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := svc.GetCandles(context.Background(), 7, 3600, from, to); err != nil {
		t.Fatalf("GetCandles: %v", err)
	}
	if repo.gotMarketID != 7 || repo.gotIntervalSec != 3600 {
		t.Fatalf("repo got marketID=%d intervalSec=%d, want 7, 3600", repo.gotMarketID, repo.gotIntervalSec)
	}
	if !repo.gotFrom.Equal(from) || !repo.gotTo.Equal(to) {
		t.Fatalf("repo got from=%v to=%v, want %v, %v", repo.gotFrom, repo.gotTo, from, to)
	}
}

func TestGetCandlesMapsRepositoryRows(t *testing.T) {
	repo := &fakeCandleRepository{rows: []repository.Candle{
		{BucketStart: 1000, Open: 100, High: 120, Low: 90, Close: 110, Volume: 5, TradeCount: 3},
	}}
	svc := newTestService(repo)

	got, err := svc.GetCandles(context.Background(), 1, 60, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("GetCandles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	c := got[0]
	if c.BucketStart != 1000 || c.Open != 100 || c.High != 120 || c.Low != 90 || c.Close != 110 || c.Volume != 5 {
		t.Fatalf("got = %+v, unexpected mapping", c)
	}
}

func TestGetCandlesRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeCandleRepository{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.GetCandles(context.Background(), 1, 60, time.Now(), time.Now())
	if !errors.Is(err, candles.ErrFetchFailed) {
		t.Fatalf("err = %v, want ErrFetchFailed", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}
