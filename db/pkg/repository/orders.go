package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrOrderNotFound          = errors.New("order not found")
	ErrDuplicateClientOrderID = errors.New("order with this client_order_id already exists for this user")
)

type OrderRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

type OrderRow struct {
	ID                   uuid.UUID
	ClientOrderID        string
	UserID               uuid.UUID
	HaveInstrumentID     int
	WantInstrumentID     int
	HaveQuantity         uint64
	WantQuantity         uint64
	CreatedAt            int64
	ExpiresAt            *int64
	Type                 string
	TimeInForce          string
	Price                *uint64
	MarketID             *int
	Side                 *string
	ORemainingHaveAmount *uint64
	ORemainingWantAmount *uint64
	CancelledAt          *int64
	CRemainingHaveAmount *uint64
	CRemainingWantAmount *uint64
}

type InsertOrderParams struct {
	ID               uuid.UUID
	ClientOrderID    *string
	UserID           uuid.UUID
	HaveInstrumentID int
	WantInstrumentID int
	HaveQuantity     *uint64
	WantQuantity     *uint64
	// Status is the order lifecycle state persisted in the orders table. It must be
	// one of the OrderStatus* constants. Callers that build params for the matcher's
	// batch flush set it from the matching outcome; ProcessBatch sets it to
	// OrderStatusCancelled for orders rejected at reservation time.
	Status      string
	Type        string
	TimeInForce string
	ExpiresAt   *time.Time
}

type InsertOpenOrderParams struct {
	OrderID             uuid.UUID
	Price               uint64
	MarketID            int
	Side                string
	RemainingHaveAmount uint64
	RemainingWantAmount uint64
}

// getOrder is an internal helper called only by GetOrderById and GetOrderByClientOrderID.
// where must be a hardcoded SQL fragment (e.g. "WHERE orders.id = $1") — never user input.
func (o *OrderRepository) getOrder(ctx context.Context, where string, args ...any) (*OrderRow, error) {
	query := `
		SELECT
			orders.id,
			orders.client_order_id,
			orders.user_id,
			orders.have_instrument_id,
			orders.want_instrument_id,
			orders.have_quantity,
			orders.want_quantity,
			orders.created_at,
			orders.expires_at,
			orders.type,
			orders.time_in_force,
			open_orders.price,
			open_orders.market_id,
			open_orders.side,
			open_orders.remaining_have_amount,
			open_orders.remaining_want_amount,
			cancelled_orders.remaining_have_amount,
			cancelled_orders.remaining_want_amount,
			cancelled_orders.cancelled_at
		FROM orders
		LEFT JOIN open_orders      ON open_orders.order_id      = orders.id
		LEFT JOIN cancelled_orders ON cancelled_orders.order_id = orders.id
	` + where

	var row OrderRow
	var createdAt time.Time
	var expiresAt sql.NullTime
	var cancelledAt sql.NullTime
	var clientOrderID sql.NullString   // client_order_id is nullable
	var haveQty, wantQty sql.NullInt64 // have/want_quantity are nullable (market orders)

	err := o.psql.QueryRowContext(ctx, query, args...).Scan(
		&row.ID,
		&clientOrderID,
		&row.UserID,
		&row.HaveInstrumentID,
		&row.WantInstrumentID,
		&haveQty,
		&wantQty,
		&createdAt,
		&expiresAt,
		&row.Type,
		&row.TimeInForce,
		&row.Price,
		&row.MarketID,
		&row.Side,
		&row.ORemainingHaveAmount,
		&row.ORemainingWantAmount,
		&row.CRemainingHaveAmount,
		&row.CRemainingWantAmount,
		&cancelledAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		o.logger.Error("getOrder: " + err.Error())
		return nil, fmt.Errorf("get order: %w", err)
	}

	row.CreatedAt = createdAt.Unix()
	row.ClientOrderID = clientOrderID.String
	row.HaveQuantity = uint64(haveQty.Int64)
	row.WantQuantity = uint64(wantQty.Int64)
	if expiresAt.Valid {
		v := expiresAt.Time.Unix()
		row.ExpiresAt = &v
	}
	if cancelledAt.Valid {
		v := cancelledAt.Time.Unix()
		row.CancelledAt = &v
	}

	return &row, nil
}

func (o *OrderRepository) GetOrderByID(ctx context.Context, userID uuid.UUID, id uuid.UUID) (*OrderRow, error) {
	return o.getOrder(ctx, "WHERE orders.user_id = $1 AND orders.id = $2", userID, id)
}

func (o *OrderRepository) GetOrderByClientOrderID(ctx context.Context, userID uuid.UUID, clientOrderID string) (*OrderRow, error) {
	return o.getOrder(ctx, "WHERE orders.user_id = $1 AND orders.client_order_id = $2", userID, clientOrderID)
}

func (o *OrderRepository) GetOrdersByUser(
	ctx context.Context,
	userID uuid.UUID,
	showOpenOrders bool,
	showCanceledOrders bool,
	baseInstrumentID, quoteInstrumentID *int,
	startDate, endDate *time.Time,
	limit int,
) ([]OrderRow, error) {
	// Base columns always selected from orders.
	cols := []string{
		"orders.id",
		"orders.client_order_id",
		"orders.user_id",
		"orders.have_instrument_id",
		"orders.want_instrument_id",
		"orders.have_quantity",
		"orders.want_quantity",
		"orders.created_at",
		"orders.expires_at",
		"orders.type",
		"orders.time_in_force",
	}
	var joins []string

	if showOpenOrders {
		cols = append(cols,
			"open_orders.price",
			"open_orders.market_id",
			"open_orders.side",
			"open_orders.remaining_have_amount",
			"open_orders.remaining_want_amount",
		)
		joins = append(joins, "LEFT JOIN open_orders ON open_orders.order_id = orders.id")
	}

	if showCanceledOrders {
		cols = append(cols,
			"cancelled_orders.remaining_have_amount",
			"cancelled_orders.remaining_want_amount",
			"cancelled_orders.cancelled_at",
		)
		joins = append(joins, "LEFT JOIN cancelled_orders ON cancelled_orders.order_id = orders.id")
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(cols, ", "))
	sb.WriteString("\nFROM orders")
	for _, j := range joins {
		sb.WriteByte('\n')
		sb.WriteString(j)
	}
	args := []any{userID}
	sb.WriteString("\nWHERE orders.user_id = $1")

	if startDate != nil {
		args = append(args, *startDate)
		sb.WriteString(fmt.Sprintf("\nAND orders.created_at >= $%d", len(args)))
	}

	if endDate != nil {
		args = append(args, *endDate)
		sb.WriteString(fmt.Sprintf("\nAND orders.created_at < $%d", len(args)))
	}

	if baseInstrumentID != nil && quoteInstrumentID != nil {
		baseIdx := len(args) + 1
		quoteIdx := len(args) + 2
		args = append(args, *baseInstrumentID, *quoteInstrumentID)
		sb.WriteString(fmt.Sprintf(
			"\nAND ((orders.have_instrument_id = $%d AND orders.want_instrument_id = $%d)"+
				" OR (orders.have_instrument_id = $%d AND orders.want_instrument_id = $%d))",
			baseIdx, quoteIdx, quoteIdx, baseIdx,
		))
	}

	args = append(args, limit)
	sb.WriteString(fmt.Sprintf("\nORDER BY orders.created_at DESC\nLIMIT $%d", len(args)))

	dbRows, err := o.psql.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		o.logger.Error("GetOrdersByUser: " + err.Error())
		return nil, fmt.Errorf("get orders by user: %w", err)
	}
	defer dbRows.Close()

	result := make([]OrderRow, 0, limit)
	for dbRows.Next() {
		var row OrderRow

		// DB stores timestamps; OrderRow keeps Unix seconds.
		// Use intermediate vars so Scan gets the right target type.
		var createdAt time.Time
		var expiresAt sql.NullTime
		var cancelledAt sql.NullTime
		var clientOrderID sql.NullString   // client_order_id is nullable
		var haveQty, wantQty sql.NullInt64 // have/want_quantity are nullable (market orders)

		scanArgs := []any{
			&row.ID,
			&clientOrderID,
			&row.UserID,
			&row.HaveInstrumentID,
			&row.WantInstrumentID,
			&haveQty,
			&wantQty,
			&createdAt,
			&expiresAt,
			&row.Type,
			&row.TimeInForce,
		}
		if showOpenOrders {
			scanArgs = append(scanArgs,
				&row.Price,
				&row.MarketID,
				&row.Side,
				&row.ORemainingHaveAmount,
				&row.ORemainingWantAmount,
			)
		}
		if showCanceledOrders {
			scanArgs = append(scanArgs,
				&row.CRemainingHaveAmount,
				&row.CRemainingWantAmount,
				&cancelledAt,
			)
		}

		if err := dbRows.Scan(scanArgs...); err != nil {
			o.logger.Error("GetOrdersByUser: scan: " + err.Error())
			return nil, fmt.Errorf("get orders by user: scan: %w", err)
		}

		row.CreatedAt = createdAt.Unix()
		row.ClientOrderID = clientOrderID.String
		row.HaveQuantity = uint64(haveQty.Int64)
		row.WantQuantity = uint64(wantQty.Int64)
		if expiresAt.Valid {
			v := expiresAt.Time.Unix()
			row.ExpiresAt = &v
		}
		if cancelledAt.Valid {
			v := cancelledAt.Time.Unix()
			row.CancelledAt = &v
		}

		result = append(result, row)
	}
	if err := dbRows.Err(); err != nil {
		o.logger.Error("GetOrdersByUser: " + err.Error())
		return nil, fmt.Errorf("get orders by user: %w", err)
	}

	return result, nil
}

func (o *OrderRepository) GetOrdersByIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]OrderRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	const query = `
		SELECT
			orders.id,
			orders.client_order_id,
			orders.user_id,
			orders.have_instrument_id,
			orders.want_instrument_id,
			orders.have_quantity,
			orders.want_quantity,
			orders.created_at,
			orders.expires_at,
			orders.type,
			orders.time_in_force,
			open_orders.price,
			open_orders.market_id,
			open_orders.side,
			open_orders.remaining_have_amount,
			open_orders.remaining_want_amount,
			cancelled_orders.remaining_have_amount,
			cancelled_orders.remaining_want_amount,
			cancelled_orders.cancelled_at
		FROM orders
		LEFT JOIN open_orders      ON open_orders.order_id      = orders.id
		LEFT JOIN cancelled_orders ON cancelled_orders.order_id = orders.id
		WHERE orders.user_id = $1 AND orders.id = ANY($2::uuid[])`

	rows, err := o.psql.QueryContext(ctx, query, userID, pq.Array(uuidStrings(ids)))
	if err != nil {
		o.logger.Error("GetOrdersByIDs: " + err.Error())
		return nil, fmt.Errorf("get orders by ids: %w", err)
	}
	defer rows.Close()

	result := make([]OrderRow, 0, len(ids))
	for rows.Next() {
		var row OrderRow
		var createdAt time.Time
		var expiresAt sql.NullTime
		var cancelledAt sql.NullTime
		var clientOrderID sql.NullString
		var haveQty, wantQty sql.NullInt64

		if err := rows.Scan(
			&row.ID,
			&clientOrderID,
			&row.UserID,
			&row.HaveInstrumentID,
			&row.WantInstrumentID,
			&haveQty,
			&wantQty,
			&createdAt,
			&expiresAt,
			&row.Type,
			&row.TimeInForce,
			&row.Price,
			&row.MarketID,
			&row.Side,
			&row.ORemainingHaveAmount,
			&row.ORemainingWantAmount,
			&row.CRemainingHaveAmount,
			&row.CRemainingWantAmount,
			&cancelledAt,
		); err != nil {
			o.logger.Error("GetOrdersByIDs: scan: " + err.Error())
			return nil, fmt.Errorf("get orders by ids: scan: %w", err)
		}

		row.CreatedAt = createdAt.Unix()
		row.ClientOrderID = clientOrderID.String
		row.HaveQuantity = uint64(haveQty.Int64)
		row.WantQuantity = uint64(wantQty.Int64)
		if expiresAt.Valid {
			v := expiresAt.Time.Unix()
			row.ExpiresAt = &v
		}
		if cancelledAt.Valid {
			v := cancelledAt.Time.Unix()
			row.CancelledAt = &v
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		o.logger.Error("GetOrdersByIDs: " + err.Error())
		return nil, fmt.Errorf("get orders by ids: %w", err)
	}
	return result, nil
}

func NewOrderRepository(logger *logger.Logger, psql *sql.DB) *OrderRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &OrderRepository{
		psql:   psql,
		logger: logger,
	}
}
