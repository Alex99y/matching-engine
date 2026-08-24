package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/google/uuid"
)

type Match struct {
	ID        uuid.UUID
	Price     uint64
	Quantity  uint64
	TakerSide string
	MatchTime time.Time
}

type MatchRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

func (r *MatchRepository) GetMatches(ctx context.Context, marketID int, startDate, endDate *time.Time, limit int) ([]Match, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT id, match_price, match_buy_amount, buy_order_is_taker, match_time
		FROM matches
		WHERE market_id = $1`)
	args := []any{marketID}

	if startDate != nil {
		args = append(args, *startDate)
		sb.WriteString(fmt.Sprintf("\nAND match_time >= $%d", len(args)))
	}
	if endDate != nil {
		args = append(args, *endDate)
		sb.WriteString(fmt.Sprintf("\nAND match_time < $%d", len(args)))
	}

	args = append(args, limit)
	sb.WriteString(fmt.Sprintf("\nORDER BY match_time DESC\nLIMIT $%d", len(args)))

	rows, err := r.psql.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		r.logger.Error(fmt.Sprintf("match repository: get matches market=%d: %v", marketID, err))
		return nil, fmt.Errorf("get matches: %w", err)
	}
	defer rows.Close()

	var matches []Match
	for rows.Next() {
		var m Match
		var buyOrderIsTaker bool
		if err := rows.Scan(&m.ID, &m.Price, &m.Quantity, &buyOrderIsTaker, &m.MatchTime); err != nil {
			r.logger.Error(fmt.Sprintf("match repository: scan match: %v", err))
			return nil, fmt.Errorf("scan match: %w", err)
		}
		m.TakerSide = "sell"
		if buyOrderIsTaker {
			m.TakerSide = "buy"
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

func NewMatchRepository(log *logger.Logger, psql *sql.DB) *MatchRepository {
	if log == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &MatchRepository{psql: psql, logger: log}
}
