package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
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

type Market struct {
	ID            int
	BaseSymbol    string
	QuoteSymbol   string
	PriceQuantum  int64
	AmountQuantum int64
	MinOrderSize  int64
	MaxOrderSize  int64
}

type MarketRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

func (r *MarketRepository) CreateMarket(
	ctx context.Context,
	baseSymbol, quoteSymbol string,
	priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64,
) error {
	// INSERT ... SELECT avoids a separate lookup round-trip.
	// If either symbol is missing the SELECT returns 0 rows → rowsAffected == 0.
	query := `
		INSERT INTO markets (base_instrument_id, quote_instrument_id, price_quantum, amount_quantum, min_order_size, max_order_size)
		SELECT bi.id, qi.id, $3, $4, $5, $6
		FROM instruments bi, instruments qi
		WHERE bi.symbol = $1 AND qi.symbol = $2
	`
	result, err := r.psql.ExecContext(ctx, query, baseSymbol, quoteSymbol, priceQuantum, amountQuantum, minOrderSize, maxOrderSize)
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
	query := `
		SELECT m.id, bi.symbol, qi.symbol,
		       m.price_quantum, m.amount_quantum, m.min_order_size, m.max_order_size
		FROM markets m
		JOIN instruments bi ON m.base_instrument_id = bi.id
		JOIN instruments qi ON m.quote_instrument_id = qi.id
		WHERE bi.symbol = $1 AND qi.symbol = $2
	`
	row := r.psql.QueryRowContext(ctx, query, baseSymbol, quoteSymbol)
	m := &Market{}

	err := row.Scan(&m.ID, &m.BaseSymbol, &m.QuoteSymbol, &m.PriceQuantum, &m.AmountQuantum, &m.MinOrderSize, &m.MaxOrderSize)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketNotFound)
		}
		r.logger.Error("error scanning market")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}

	return m, nil
}

func (r *MarketRepository) GetMarkets(ctx context.Context) ([]Market, error) {
	query := `
		SELECT m.id, bi.symbol, qi.symbol,
		       m.price_quantum, m.amount_quantum, m.min_order_size, m.max_order_size
		FROM markets m
		JOIN instruments bi ON m.base_instrument_id = bi.id
		JOIN instruments qi ON m.quote_instrument_id = qi.id
		ORDER BY bi.symbol ASC, qi.symbol ASC
	`
	rows, err := r.psql.QueryContext(ctx, query)
	if err != nil {
		r.logger.Error("error querying markets")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}
	defer rows.Close()

	markets := []Market{}
	for rows.Next() {
		var m Market
		if err := rows.Scan(&m.ID, &m.BaseSymbol, &m.QuoteSymbol, &m.PriceQuantum, &m.AmountQuantum, &m.MinOrderSize, &m.MaxOrderSize); err != nil {
			r.logger.Error("error scanning market row")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
		}
		markets = append(markets, m)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating market rows")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", marketErrPrefix, ErrMarketGetFailed)
	}

	return markets, nil
}

func (r *MarketRepository) RemoveOneMarket(ctx context.Context, baseSymbol, quoteSymbol string) error {
	// Subqueries return NULL if a symbol is missing → WHERE col = NULL matches nothing → 0 rows deleted.
	query := `
		DELETE FROM markets
		WHERE base_instrument_id  = (SELECT id FROM instruments WHERE symbol = $1)
		  AND quote_instrument_id = (SELECT id FROM instruments WHERE symbol = $2)
	`
	result, err := r.psql.ExecContext(ctx, query, baseSymbol, quoteSymbol)
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

func NewMarketRepository(logger *logger.Logger, psql *sql.DB) *MarketRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &MarketRepository{
		psql:   psql,
		logger: logger,
	}
}
