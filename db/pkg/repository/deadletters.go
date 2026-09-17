package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/utils"
	"github.com/google/uuid"
)

type InsertDeadLetterParams struct {
	MessageID string
	MarketRef string
	OrderID   *uuid.UUID
	EventType string
	Reason    string
	Error     string
	Payload   json.RawMessage
	DeadAt    time.Time
}

type DeadLetterRow struct {
	ID         int64
	MessageID  string
	MarketRef  string
	OrderID    *uuid.UUID
	EventType  string
	Reason     string
	Error      string
	Payload    json.RawMessage
	DeadAt     time.Time
	RecordedAt time.Time
	Status     string
}

type DeadLetterRepository struct {
	psql    *sql.DB
	logger  *logger.Logger
	timeout time.Duration
}

// InsertDeadLetter is idempotent on (market, message id, event type, dead-at): the consumer that
// calls it may see the same broker delivery twice after a crash between insert and ack, and the
// second one must add nothing. A message with no id cannot be told apart and is always inserted.
func (r *DeadLetterRepository) InsertDeadLetter(ctx context.Context, p InsertDeadLetterParams) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	const q = `
		INSERT INTO dead_letters
			(message_id, market_ref, order_id, event_type, reason, error, payload, dead_at)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8
		WHERE $1 = '' OR NOT EXISTS (
			SELECT 1 FROM dead_letters
			WHERE market_ref = $2 AND message_id = $1 AND event_type = $4 AND dead_at = $8
		)`
	// lib/pq encodes []byte as bytea, which JSONB rejects; the payload travels as text.
	_, err := r.psql.ExecContext(ctxWithTimeout, q,
		p.MessageID, p.MarketRef, nullUUID(p.OrderID), p.EventType, p.Reason, p.Error, string(p.Payload), p.DeadAt.UTC())
	if err != nil {
		r.logger.Error(fmt.Sprintf("dead letter repository: insert market=%s message=%s: %v", p.MarketRef, p.MessageID, err))
		return fmt.Errorf("insert dead letter: %w", err)
	}
	return nil
}

// ListDeadLetters returns the newest first, across every market when marketRef is nil.
func (r *DeadLetterRepository) ListDeadLetters(ctx context.Context, marketRef *string, limit int) ([]DeadLetterRow, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var sb strings.Builder
	sb.WriteString(`
		SELECT id, message_id, market_ref, order_id, event_type, reason, error, payload, dead_at, recorded_at, status
		FROM dead_letters`)
	var args []any
	if marketRef != nil {
		args = append(args, *marketRef)
		sb.WriteString(fmt.Sprintf("\nWHERE market_ref = $%d", len(args)))
	}
	args = append(args, limit)
	sb.WriteString(fmt.Sprintf("\nORDER BY recorded_at DESC, id DESC\nLIMIT $%d", len(args)))

	rows, err := r.psql.QueryContext(ctxWithTimeout, sb.String(), args...)
	if err != nil {
		r.logger.Error(fmt.Sprintf("dead letter repository: list: %v", err))
		return nil, fmt.Errorf("list dead letters: %w", err)
	}
	defer rows.Close()

	out := make([]DeadLetterRow, 0, limit)
	for rows.Next() {
		var row DeadLetterRow
		var orderID uuid.NullUUID
		var payload []byte
		if err := rows.Scan(
			&row.ID, &row.MessageID, &row.MarketRef, &orderID, &row.EventType, &row.Reason, &row.Error,
			&payload, &row.DeadAt, &row.RecordedAt, &row.Status,
		); err != nil {
			r.logger.Error(fmt.Sprintf("dead letter repository: scan: %v", err))
			return nil, fmt.Errorf("scan dead letter: %w", err)
		}
		if orderID.Valid {
			id := orderID.UUID
			row.OrderID = &id
		}
		row.Payload = json.RawMessage(payload)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error(fmt.Sprintf("dead letter repository: list: %v", err))
		return nil, fmt.Errorf("list dead letters: %w", err)
	}
	return out, nil
}

func NewDeadLetterRepository(log *logger.Logger, psql *sql.DB, timeout time.Duration) *DeadLetterRepository {
	if log == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	utils.ValidateTimeout("dead letter repository", timeout)
	return &DeadLetterRepository{psql: psql, logger: log, timeout: timeout}
}
