package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	dbutils "github.com/alex99y/matching-engine/db/pkg/utils"
)

const marketErrPrefix = "market repository:"

var (
	ErrMarketAlreadyExists = errors.New("market already exists")
	ErrMarketInsertFailed  = errors.New("failed to insert market")
	ErrMarketNotFound      = errors.New("market not found")
	ErrMarketGetFailed     = errors.New("failed to get market")
	ErrMarketDeleteFailed  = errors.New("failed to delete market")
	ErrInvalidInstruments  = errors.New("one or more instruments not found")
)

// markets_base_quote_uk is defined explicitly in the migration DDL.
const MarketBaseQuoteUniqueConstraint = "markets_base_quote_uk"

type MarketPrice struct {
	BaseSymbol   string
	QuoteSymbol  string
	Price        *uint64 // nil when the market has no matches yet
	MinPrice24h  *uint64
	MaxPrice24h  *uint64
	Volume24h    *uint64
	OpenPrice24h *uint64 // nil when the market has no matches within the last 24h
}

type Market struct {
	ID                int
	BaseSymbol        string
	QuoteSymbol       string
	PriceQuantum      uint64
	AmountQuantum     uint64
	MinOrderSize      uint64
	MaxOrderSize      uint64
	BaseInstrumentID  int
	QuoteInstrumentID int
	TakerFeeBps       uint64
	MakerFeeBps       uint64
	// BaseScale is 10^baseDecimals. It normalises price×qty back to quote-quanta:
	//   notional_in_quote_quanta = price × qty / BaseScale
	// Price is in quote-quanta per whole base coin (e.g. nano-USDT per BTC);
	// qty is in base-quanta (e.g. nano-BTC). Without this division the product is
	// 10^baseDecimals times larger than a real quote amount.
	BaseScale uint64
}

type MarketRepository struct {
	psql    *sql.DB
	logger  *logger.Logger
	timeout time.Duration
}

func (r *MarketRepository) CreateMarket(
	ctx context.Context,
	baseSymbol, quoteSymbol string,
	priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64,
	takerFeeBps, makerFeeBps int64,
) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// INSERT ... SELECT avoids a separate lookup round-trip.
	// If either symbol is missing the SELECT returns 0 rows → rowsAffected == 0.
	query := `
		INSERT INTO markets (base_instrument_id, quote_instrument_id, price_quantum, amount_quantum, min_order_size, max_order_size, taker_fee_bps, maker_fee_bps)
		SELECT bi.id, qi.id, $3, $4, $5, $6, $7, $8
		FROM instruments bi, instruments qi
		WHERE bi.symbol = $1 AND qi.symbol = $2
	`
	result, err := r.psql.ExecContext(ctxWithTimeout, query, baseSymbol, quoteSymbol, priceQuantum, amountQuantum, minOrderSize, maxOrderSize, takerFeeBps, makerFeeBps)
	if err != nil {
		if constraint, isUnique := postgres.IsUniqueConstraintViolation(err); isUnique {
			if constraint == MarketBaseQuoteUniqueConstraint {
				return fmt.Errorf("%s %w", marketErrPrefix, ErrMarketAlreadyExists)
			}
		}
		r.logger.Error("error inserting market")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", marketErrPrefix, ErrMarketInsertFailed)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error getting rows affected for market insert")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", marketErrPrefix, ErrMarketInsertFailed)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%s %w", marketErrPrefix, ErrInvalidInstruments)
	}
	return nil
}

func (r *MarketRepository) GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*Market, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query := `
		SELECT m.id, bi.symbol, qi.symbol,
		       m.price_quantum, m.amount_quantum, m.min_order_size, m.max_order_size,
		       m.base_instrument_id, m.quote_instrument_id,
		       m.taker_fee_bps, m.maker_fee_bps,
		       bi.decimals AS base_decimals
		FROM markets m
		JOIN instruments bi ON m.base_instrument_id = bi.id
		JOIN instruments qi ON m.quote_instrument_id = qi.id
		WHERE bi.symbol = $1 AND qi.symbol = $2
	`
	row := r.psql.QueryRowContext(ctxWithTimeout, query, baseSymbol, quoteSymbol)
	m := &Market{}
	var baseDecimals int

	err := row.Scan(&m.ID, &m.BaseSymbol, &m.QuoteSymbol, &m.PriceQuantum, &m.AmountQuantum, &m.MinOrderSize, &m.MaxOrderSize, &m.BaseInstrumentID, &m.QuoteInstrumentID, &m.TakerFeeBps, &m.MakerFeeBps, &baseDecimals)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketNotFound)
		}
		r.logger.Error("error scanning market")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}
	m.BaseScale = utils.Pow10Uint64(baseDecimals)

	return m, nil
}

func (r *MarketRepository) GetMarkets(ctx context.Context) ([]Market, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query := `
		SELECT m.id, bi.symbol, qi.symbol,
		       m.price_quantum, m.amount_quantum, m.min_order_size, m.max_order_size,
		       m.base_instrument_id, m.quote_instrument_id,
		       m.taker_fee_bps, m.maker_fee_bps,
		       bi.decimals AS base_decimals
		FROM markets m
		JOIN instruments bi ON m.base_instrument_id = bi.id
		JOIN instruments qi ON m.quote_instrument_id = qi.id
		ORDER BY bi.symbol ASC, qi.symbol ASC
	`
	rows, err := r.psql.QueryContext(ctxWithTimeout, query)
	if err != nil {
		r.logger.Error("error querying markets")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}
	defer rows.Close()

	markets := []Market{}
	for rows.Next() {
		var m Market
		var baseDecimals int
		if err := rows.Scan(&m.ID, &m.BaseSymbol, &m.QuoteSymbol, &m.PriceQuantum, &m.AmountQuantum, &m.MinOrderSize, &m.MaxOrderSize, &m.BaseInstrumentID, &m.QuoteInstrumentID, &m.TakerFeeBps, &m.MakerFeeBps, &baseDecimals); err != nil {
			r.logger.Error("error scanning market row")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
		}
		m.BaseScale = utils.Pow10Uint64(baseDecimals)
		markets = append(markets, m)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating market rows")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}

	return markets, nil
}

func (r *MarketRepository) GetLatestPrices(ctx context.Context, windowStart time.Time) ([]MarketPrice, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, rollback, err := dbutils.BeginTx(ctxWithTimeout, r.psql, r.logger, "GetLatestPrices")
	if err != nil {
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}
	defer rollback()

	// This query's cost estimate crosses jit_above_cost even though it only ever returns a
	// handful of rows; SET LOCAL (not SET) keeps the exemption from leaking onto whatever
	// unrelated query next borrows this pooled connection.
	if _, err := tx.ExecContext(ctxWithTimeout, "SET LOCAL jit = off"); err != nil {
		r.logger.Error("error disabling jit for latest prices query")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}

	query := `
		SELECT bi.symbol, qi.symbol,
		       lp.match_price,
		       stats.min_price, stats.max_price, stats.volume,
		       openp.match_price
		FROM markets m
		JOIN instruments bi ON bi.id = m.base_instrument_id
		JOIN instruments qi ON qi.id = m.quote_instrument_id
		LEFT JOIN LATERAL (
			SELECT match_price
			FROM matches
			WHERE matches.market_id = m.id
			ORDER BY match_time DESC, id DESC
			LIMIT 1
		) lp ON true
		LEFT JOIN LATERAL (
			SELECT MIN(match_price) AS min_price, MAX(match_price) AS max_price,
			       SUM(match_buy_amount) AS volume
			FROM matches
			WHERE matches.market_id = m.id AND match_time >= $1
		) stats ON true
		LEFT JOIN LATERAL (
			SELECT match_price
			FROM matches
			WHERE matches.market_id = m.id AND match_time >= $1
			ORDER BY match_time ASC, id ASC
			LIMIT 1
		) openp ON true
		ORDER BY bi.symbol ASC, qi.symbol ASC
	`
	rows, err := tx.QueryContext(ctxWithTimeout, query, windowStart)
	if err != nil {
		r.logger.Error("error querying latest prices")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}
	defer rows.Close()

	prices := []MarketPrice{}
	for rows.Next() {
		var p MarketPrice
		var price, minPrice, maxPrice, volume, openPrice sql.NullInt64
		if err := rows.Scan(&p.BaseSymbol, &p.QuoteSymbol, &price, &minPrice, &maxPrice, &volume, &openPrice); err != nil {
			r.logger.Error("error scanning latest price row")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
		}
		p.Price = dbutils.NullInt64ToUint64(price)
		p.MinPrice24h = dbutils.NullInt64ToUint64(minPrice)
		p.MaxPrice24h = dbutils.NullInt64ToUint64(maxPrice)
		p.Volume24h = dbutils.NullInt64ToUint64(volume)
		p.OpenPrice24h = dbutils.NullInt64ToUint64(openPrice)
		prices = append(prices, p)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating latest price rows")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}

	return prices, nil
}

func (r *MarketRepository) RemoveOneMarket(ctx context.Context, baseSymbol, quoteSymbol string) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Subqueries return NULL if a symbol is missing → WHERE col = NULL matches nothing → 0 rows deleted.
	query := `
		DELETE FROM markets
		WHERE base_instrument_id  = (SELECT id FROM instruments WHERE symbol = $1)
		  AND quote_instrument_id = (SELECT id FROM instruments WHERE symbol = $2)
	`
	result, err := r.psql.ExecContext(ctxWithTimeout, query, baseSymbol, quoteSymbol)
	if err != nil {
		r.logger.Error("error deleting market")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", marketErrPrefix, ErrMarketDeleteFailed)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error getting rows affected for market delete")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", marketErrPrefix, ErrMarketDeleteFailed)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%s %w", marketErrPrefix, ErrMarketNotFound)
	}
	return nil
}

func NewMarketRepository(logger *logger.Logger, psql *sql.DB, timeout time.Duration) *MarketRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	dbutils.ValidateTimeout("market repository", timeout)
	return &MarketRepository{
		psql:    psql,
		logger:  logger,
		timeout: timeout,
	}
}
