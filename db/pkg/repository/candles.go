package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
)

var ErrNoCandle = errors.New("no trades in candle range")

type Candle struct {
	BucketStart int64
	Open        uint64
	High        uint64
	Low         uint64
	Close       uint64
	Volume      uint64
	TradeCount  int64
}

type CandleRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

func (r *CandleRepository) GetCandles(ctx context.Context, marketID int, intervalSec int64, from, to time.Time) ([]Candle, error) {
	const q = `
		SELECT
			extract(epoch from match_time)::bigint / $1 * $1              AS bucket,
			(array_agg(match_price ORDER BY match_time ASC))[1]           AS open,
			MAX(match_price)                                               AS high,
			MIN(match_price)                                               AS low,
			(array_agg(match_price ORDER BY match_time DESC))[1]          AS close,
			SUM(match_buy_amount)                                          AS volume,
			COUNT(*)                                                       AS trade_count
		FROM matches
		WHERE market_id = $2
		  AND match_time >= $3
		  AND match_time < $4
		GROUP BY bucket
		ORDER BY bucket`

	rows, err := r.psql.QueryContext(ctx, q, intervalSec, marketID, from, to)
	if err != nil {
		r.logger.Error(fmt.Sprintf("candle repository: get candles market=%d interval=%d: %v", marketID, intervalSec, err))
		return nil, fmt.Errorf("get candles: %w", err)
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.BucketStart, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.TradeCount); err != nil {
			r.logger.Error(fmt.Sprintf("candle repository: scan candle: %v", err))
			return nil, fmt.Errorf("scan candle: %w", err)
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}

// GetCurrentCandle returns the partial candle for the bucket starting at bucketStart.
// It runs inside a REPEATABLE READ transaction so the DB snapshot timestamp aligns with
// the CandleHub's channel-buffer cutoff, eliminating overlap between the seed result
// and the buffered trade events delivered after registration.
func (r *CandleRepository) GetCurrentCandle(ctx context.Context, marketID int, bucketStart time.Time) (*Candle, error) {
	tx, err := r.psql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin repeatable read: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	const q = `
		SELECT
			(array_agg(match_price ORDER BY match_time ASC))[1]           AS open,
			MAX(match_price)                                               AS high,
			MIN(match_price)                                               AS low,
			(array_agg(match_price ORDER BY match_time DESC))[1]          AS close,
			SUM(match_buy_amount)                                          AS volume,
			COUNT(*)                                                       AS trade_count
		FROM matches
		WHERE market_id = $1
		  AND match_time >= $2
		HAVING COUNT(*) > 0`

	var c Candle
	c.BucketStart = bucketStart.Unix()
	err = tx.QueryRowContext(ctx, q, marketID, bucketStart).Scan(
		&c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.TradeCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoCandle
	}
	if err != nil {
		r.logger.Error(fmt.Sprintf("candle repository: get current candle market=%d: %v", marketID, err))
		return nil, fmt.Errorf("get current candle: %w", err)
	}
	return &c, nil
}

func NewCandleRepository(log *logger.Logger, db *sql.DB) *CandleRepository {
	if log == nil {
		panic("logger cannot be nil")
	}
	if db == nil {
		panic("db cannot be nil")
	}
	return &CandleRepository{psql: db, logger: log}
}
