package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrBatchBegin  = errors.New("begin batch transaction failed")
	ErrBatchFlush  = errors.New("flush batch failed")
	ErrBatchCommit = errors.New("commit batch failed")
	ErrReserve     = errors.New("reserve balance failed")
)

// Canonical orders.status values. They mirror the CHECK constraint in the migration.
const (
	OrderStatusOpen            = "open"
	OrderStatusFilled          = "filled"
	OrderStatusPartiallyFilled = "partially_filled"
	OrderStatusCancelled       = "cancelled"
)

// ReserveRequest is the entry-side fund reservation for one incoming order: Amount
// of the user's `have` instrument is moved balance -> blocked, conditional on the
// user having enough available balance. A failed reservation rejects the order; it
// is an expected outcome, not a transaction error.
type ReserveRequest struct {
	InstrumentID int
	Amount       uint64
}

// IncomingOrder couples an order's persistent row with its reservation requirement.
// The matcher builds one per order pulled from the channel before calling ProcessBatch.
type IncomingOrder struct {
	Insert  InsertOrderParams
	Reserve ReserveRequest
}

// OrderStatusUpdate transitions an already-persisted order (a resting maker from a
// previous batch) to a new status.
type OrderStatusUpdate struct {
	OrderID uuid.UUID
	Status  string
}

// OpenOrderRemainingUpdate updates the remaining amounts of a resting order that was
// partially consumed in this batch.
type OpenOrderRemainingUpdate struct {
	OrderID             uuid.UUID
	RemainingHaveAmount uint64
	RemainingWantAmount uint64
}

// InsertCancelledOrderParams records the unfilled remainder of a killed order
// (IOC/FOK/market remainder, reservation rejection, or user cancel).
type InsertCancelledOrderParams struct {
	OrderID             uuid.UUID
	RemainingHaveAmount uint64
	RemainingWantAmount uint64
}

// InsertMatchParams is one fill between a buy and a sell order. Buyer/seller are
// resolved from order sides by the matcher; fees are zero until a fee model exists.
type InsertMatchParams struct {
	MarketID          int
	BuyOrderID        uuid.UUID
	SellOrderID       uuid.UUID
	MatchBuyAmount    uint64
	MatchSellAmount   uint64
	MatchPrice        uint64
	MatchBuyFees      uint64
	MatchSellFees     uint64
	BuyOrderIsTaker   bool
	IsBuyOrderFilled  bool
	IsSellOrderFilled bool
}

// BalanceDelta is a signed movement applied to one user_balances row at settlement.
// BalanceDelta credits/debits available balance; BlockedDelta releases/consumes the
// previously reserved amount.
type BalanceDelta struct {
	UserID       uuid.UUID
	InstrumentID int
	BalanceDelta int64
	BlockedDelta int64
}

type balanceKey struct {
	userID       uuid.UUID
	instrumentID int
}

// BatchResult accumulates every persistent side-effect produced by matching one
// batch of orders. It is db-native (no engine types) so the db module never imports
// core. The matcher fills it inside the ProcessBatch callback; ProcessBatch itself
// appends the reservation rejections before flushing.
type BatchResult struct {
	// Orders arriving in this batch (takers). One INSERT INTO orders each.
	NewOrders []InsertOrderParams
	// Status transitions for orders persisted in a previous batch (resting makers).
	StatusUpdates []OrderStatusUpdate
	// GTC remainder that came to rest. INSERT INTO open_orders.
	OpenOrders []InsertOpenOrderParams
	// Resting makers partially consumed. UPDATE open_orders SET remaining_*.
	OpenOrderUpdates []OpenOrderRemainingUpdate
	// Resting makers fully consumed or cancelled. DELETE FROM open_orders.
	ClosedOpenOrders []uuid.UUID
	// Killed orders / remainders. INSERT INTO cancelled_orders.
	CancelledOrders []InsertCancelledOrderParams
	// One row per fill. INSERT INTO matches.
	Matches []InsertMatchParams

	// Settlement movements, aggregated per (user, instrument) so the flush touches
	// each balance row at most once.
	balanceDeltas map[balanceKey]*BalanceDelta
}

// NewBatchResult returns a BatchResult ready to accumulate into. The matcher MUST
// build its result through this constructor (not a struct literal) so balance
// aggregation is initialised.
func NewBatchResult() *BatchResult {
	return &BatchResult{balanceDeltas: make(map[balanceKey]*BalanceDelta)}
}

// AddBalanceDelta merges a settlement movement into the per-(user,instrument) accumulator.
func (b *BatchResult) AddBalanceDelta(userID uuid.UUID, instrumentID int, balanceDelta, blockedDelta int64) {
	if b.balanceDeltas == nil {
		b.balanceDeltas = make(map[balanceKey]*BalanceDelta)
	}
	k := balanceKey{userID: userID, instrumentID: instrumentID}
	d, ok := b.balanceDeltas[k]
	if !ok {
		d = &BalanceDelta{UserID: userID, InstrumentID: instrumentID}
		b.balanceDeltas[k] = d
	}
	d.BalanceDelta += balanceDelta
	d.BlockedDelta += blockedDelta
}

// BalanceDeltas returns the aggregated settlement movements.
func (b *BatchResult) BalanceDeltas() []BalanceDelta {
	out := make([]BalanceDelta, 0, len(b.balanceDeltas))
	for _, d := range b.balanceDeltas {
		out = append(out, *d)
	}
	return out
}

func (b *BatchResult) merge(o *BatchResult) {
	b.NewOrders = append(b.NewOrders, o.NewOrders...)
	b.StatusUpdates = append(b.StatusUpdates, o.StatusUpdates...)
	b.OpenOrders = append(b.OpenOrders, o.OpenOrders...)
	b.OpenOrderUpdates = append(b.OpenOrderUpdates, o.OpenOrderUpdates...)
	b.ClosedOpenOrders = append(b.ClosedOpenOrders, o.ClosedOpenOrders...)
	b.CancelledOrders = append(b.CancelledOrders, o.CancelledOrders...)
	b.Matches = append(b.Matches, o.Matches...)
	for _, d := range o.balanceDeltas {
		b.AddBalanceDelta(d.UserID, d.InstrumentID, d.BalanceDelta, d.BlockedDelta)
	}
}

// MatchFunc runs the in-memory matching for the orders that were successfully funded,
// in the supplied order, and returns every resulting side-effect. It MUST NOT perform
// I/O. It mutates the in-memory book, so if ProcessBatch returns an error the caller
// must treat the book as dirty and rebuild it from the DB before retrying the batch.
type MatchFunc func(fundedOrderIDs []uuid.UUID) (*BatchResult, error)

// ProcessBatch executes one matcher batch as a single transaction:
//
//	BEGIN
//	  phase 1: skip orders already committed by a previous (redelivered) batch;
//	           reserve funds per remaining order — failures reject the order
//	  phase 2: run in-memory matching on the funded orders (the match callback)
//	  phase 3: bulk-write every side-effect
//	COMMIT
//
// On a nil return the whole batch is durably committed and every message in it may be
// acked. On any error the transaction is rolled back (the DB is left exactly as before
// the batch) and the error is returned; because the match callback has, by then,
// mutated the in-memory book, the caller MUST rebuild the book from the DB and requeue
// the batch's messages.
func (o *OrderRepository) ProcessBatch(ctx context.Context, incoming []IncomingOrder, match MatchFunc) error {
	tx, err := o.psql.BeginTx(ctx, nil)
	if err != nil {
		o.logger.Error("ProcessBatch: begin tx: " + err.Error())
		return fmt.Errorf("process batch: %w: %w", ErrBatchBegin, err)
	}
	// Safe no-op after a successful Commit; rolls back on any early return.
	defer func() { _ = tx.Rollback() }()

	// Phase 1a — idempotency. Orders already present were committed by a prior batch
	// (e.g. redelivered after a commit-ambiguous failure); never match or persist them again.
	ids := make([]uuid.UUID, len(incoming))
	for i := range incoming {
		ids[i] = incoming[i].Insert.ID
	}
	existing, err := existingOrderIDs(ctx, tx, ids)
	if err != nil {
		o.logger.Error("ProcessBatch: idempotency lookup: " + err.Error())
		return fmt.Errorf("process batch: idempotency: %w", err)
	}

	// Phase 1b — reserve funds. A rejected reservation is recorded as a cancelled order
	// and excluded from matching.
	result := NewBatchResult()
	fundedIDs := make([]uuid.UUID, 0, len(incoming))
	for i := range incoming {
		ord := incoming[i]
		if _, dup := existing[ord.Insert.ID]; dup {
			continue
		}
		ok, err := reserveBalance(ctx, tx, ord.Insert.UserID, ord.Reserve.InstrumentID, ord.Reserve.Amount)
		if err != nil {
			o.logger.Error("ProcessBatch: reserve: " + err.Error())
			return fmt.Errorf("process batch: %w: %w", ErrReserve, err)
		}
		if !ok {
			appendRejection(result, ord.Insert)
			continue
		}
		fundedIDs = append(fundedIDs, ord.Insert.ID)
	}

	// Phase 2 — pure in-memory matching of the funded orders.
	matched, err := match(fundedIDs)
	if err != nil {
		o.logger.Error("ProcessBatch: match callback: " + err.Error())
		return fmt.Errorf("process batch: match: %w", err)
	}
	if matched != nil {
		result.merge(matched)
	}

	// Phase 3 — bulk write everything.
	if err := flushBatch(ctx, tx, result); err != nil {
		o.logger.Error("ProcessBatch: flush: " + err.Error())
		return fmt.Errorf("process batch: %w: %w", ErrBatchFlush, err)
	}

	if err := tx.Commit(); err != nil {
		o.logger.Error("ProcessBatch: commit: " + err.Error())
		return fmt.Errorf("process batch: %w: %w", ErrBatchCommit, err)
	}
	return nil
}

// appendRejection records an order rejected at reservation time: a cancelled orders
// row plus a cancelled_orders remainder equal to the full order (nothing filled).
func appendRejection(result *BatchResult, insert InsertOrderParams) {
	insert.Status = OrderStatusCancelled
	result.NewOrders = append(result.NewOrders, insert)
	result.CancelledOrders = append(result.CancelledOrders, InsertCancelledOrderParams{
		OrderID:             insert.ID,
		RemainingHaveAmount: derefU64(insert.HaveQuantity),
		RemainingWantAmount: derefU64(insert.WantQuantity),
	})
}

// reserveBalance atomically moves Amount of the user's instrument from balance to
// blocked, only if the available balance covers it. Returns false (without error)
// when the balance is insufficient or the balance row does not exist. The row lock
// taken by this UPDATE is held until commit, which serialises reservations for the
// same balance row across markets.
func reserveBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, instrumentID int, amount uint64) (bool, error) {
	if amount == 0 {
		// Defensive: validation guarantees a positive reservable amount. Nothing to lock.
		return true, nil
	}
	const q = `
		UPDATE user_balances
		SET balance = balance - $3, blocked = blocked + $3
		WHERE user_id = $1 AND instrument_id = $2 AND balance >= $3
	`
	res, err := tx.ExecContext(ctx, q, userID, instrumentID, int64(amount))
	if err != nil {
		return false, fmt.Errorf("reserve balance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reserve balance: rows affected: %w", err)
	}
	return n == 1, nil
}

// existingOrderIDs returns the subset of ids already present in the orders table.
func existingOrderIDs(ctx context.Context, tx *sql.Tx, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	out := make(map[uuid.UUID]struct{}, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const q = `SELECT id FROM orders WHERE id = ANY($1::uuid[])`
	rows, err := tx.QueryContext(ctx, q, pq.Array(uuidStrings(ids)))
	if err != nil {
		return nil, fmt.Errorf("existing order ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("existing order ids: scan: %w", err)
		}
		out[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("existing order ids: %w", err)
	}
	return out, nil
}

// flushBatch writes every accumulated side-effect in FK-safe order: parent orders
// rows first, then the child tables that reference them. Each helper issues a single
// statement (one network round-trip) and is skipped when it has nothing to write.
func flushBatch(ctx context.Context, tx *sql.Tx, b *BatchResult) error {
	if err := insertOrders(ctx, tx, b.NewOrders); err != nil {
		return err
	}
	if err := updateOrderStatuses(ctx, tx, b.StatusUpdates); err != nil {
		return err
	}
	if err := insertOpenOrders(ctx, tx, b.OpenOrders); err != nil {
		return err
	}
	if err := updateOpenOrders(ctx, tx, b.OpenOrderUpdates); err != nil {
		return err
	}
	if err := deleteOpenOrders(ctx, tx, b.ClosedOpenOrders); err != nil {
		return err
	}
	if err := insertCancelledOrders(ctx, tx, b.CancelledOrders); err != nil {
		return err
	}
	if err := insertMatches(ctx, tx, b.Matches); err != nil {
		return err
	}
	return applyBalanceDeltas(ctx, tx, b.BalanceDeltas())
}

func insertOrders(ctx context.Context, tx *sql.Tx, orders []InsertOrderParams) error {
	if len(orders) == 0 {
		return nil
	}
	const cols = 11
	args := make([]any, 0, len(orders)*cols)
	for _, o := range orders {
		args = append(args,
			o.ID,
			nullVal(o.ClientOrderID),
			o.UserID,
			o.HaveInstrumentID,
			o.WantInstrumentID,
			nullU64(o.HaveQuantity),
			nullU64(o.WantQuantity),
			o.Status,
			o.Type,
			o.TimeInForce,
			nullTime(o.ExpiresAt),
		)
	}
	// ON CONFLICT guards the (rare) case of a duplicate id slipping past the
	// idempotency pre-check; the rest of the batch still commits.
	q := `INSERT INTO orders
		(id, client_order_id, user_id, have_instrument_id, want_instrument_id,
		 have_quantity, want_quantity, status, type, time_in_force, expires_at)
		VALUES ` + valuesPlaceholders(len(orders), cols) + `
		ON CONFLICT (id) DO NOTHING`
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("insert orders: %w", err)
	}
	return nil
}

func updateOrderStatuses(ctx context.Context, tx *sql.Tx, updates []OrderStatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	args := make([]any, 0, len(updates)*2)
	for _, u := range updates {
		args = append(args, u.OrderID, u.Status)
	}
	q := `UPDATE orders o SET status = v.status
		FROM (VALUES ` + valuesPlaceholdersCast(len(updates), []string{"uuid", "text"}) + `) AS v(id, status)
		WHERE o.id = v.id`
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("update order statuses: %w", err)
	}
	return nil
}

func insertOpenOrders(ctx context.Context, tx *sql.Tx, open []InsertOpenOrderParams) error {
	if len(open) == 0 {
		return nil
	}
	const cols = 6
	args := make([]any, 0, len(open)*cols)
	for _, o := range open {
		args = append(args,
			o.OrderID,
			int64(o.Price),
			o.MarketID,
			o.Side,
			int64(o.RemainingHaveAmount),
			int64(o.RemainingWantAmount),
		)
	}
	q := `INSERT INTO open_orders
		(order_id, price, market_id, side, remaining_have_amount, remaining_want_amount)
		VALUES ` + valuesPlaceholders(len(open), cols)
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("insert open orders: %w", err)
	}
	return nil
}

func updateOpenOrders(ctx context.Context, tx *sql.Tx, updates []OpenOrderRemainingUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	args := make([]any, 0, len(updates)*3)
	for _, u := range updates {
		args = append(args, u.OrderID, int64(u.RemainingHaveAmount), int64(u.RemainingWantAmount))
	}
	q := `UPDATE open_orders o
		SET remaining_have_amount = v.rh, remaining_want_amount = v.rw
		FROM (VALUES ` + valuesPlaceholdersCast(len(updates), []string{"uuid", "bigint", "bigint"}) + `) AS v(order_id, rh, rw)
		WHERE o.order_id = v.order_id`
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("update open orders: %w", err)
	}
	return nil
}

func deleteOpenOrders(ctx context.Context, tx *sql.Tx, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	const q = `DELETE FROM open_orders WHERE order_id = ANY($1::uuid[])`
	if _, err := tx.ExecContext(ctx, q, pq.Array(uuidStrings(ids))); err != nil {
		return fmt.Errorf("delete open orders: %w", err)
	}
	return nil
}

func insertCancelledOrders(ctx context.Context, tx *sql.Tx, cancelled []InsertCancelledOrderParams) error {
	if len(cancelled) == 0 {
		return nil
	}
	const cols = 3
	args := make([]any, 0, len(cancelled)*cols)
	for _, c := range cancelled {
		args = append(args, c.OrderID, int64(c.RemainingHaveAmount), int64(c.RemainingWantAmount))
	}
	q := `INSERT INTO cancelled_orders (order_id, remaining_have_amount, remaining_want_amount)
		VALUES ` + valuesPlaceholders(len(cancelled), cols)
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("insert cancelled orders: %w", err)
	}
	return nil
}

func insertMatches(ctx context.Context, tx *sql.Tx, matches []InsertMatchParams) error {
	if len(matches) == 0 {
		return nil
	}
	const cols = 11
	args := make([]any, 0, len(matches)*cols)
	for _, m := range matches {
		args = append(args,
			m.MarketID,
			m.BuyOrderID,
			m.SellOrderID,
			int64(m.MatchBuyAmount),
			int64(m.MatchSellAmount),
			int64(m.MatchPrice),
			int64(m.MatchSellFees),
			int64(m.MatchBuyFees),
			m.BuyOrderIsTaker,
			m.IsBuyOrderFilled,
			m.IsSellOrderFilled,
		)
	}
	q := `INSERT INTO matches
		(market_id, buy_order_id, sell_order_id, match_buy_amount, match_sell_amount,
		 match_price, match_sell_fees, match_buy_fees, buy_order_is_taker,
		 is_buy_order_filled, is_sell_order_filled)
		VALUES ` + valuesPlaceholders(len(matches), cols)
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("insert matches: %w", err)
	}
	return nil
}

func applyBalanceDeltas(ctx context.Context, tx *sql.Tx, deltas []BalanceDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	args := make([]any, 0, len(deltas)*4)
	for _, d := range deltas {
		args = append(args, d.UserID, d.InstrumentID, d.BalanceDelta, d.BlockedDelta)
	}
	q := `UPDATE user_balances ub
		SET balance = ub.balance + v.bal, blocked = ub.blocked + v.blk
		FROM (VALUES ` + valuesPlaceholdersCast(len(deltas), []string{"uuid", "int", "bigint", "bigint"}) + `) AS v(user_id, instrument_id, bal, blk)
		WHERE ub.user_id = v.user_id AND ub.instrument_id = v.instrument_id`
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("apply balance deltas: %w", err)
	}
	return nil
}

// OpenOrderHydration is one resting order loaded from the DB to rebuild the in-memory
// book on startup (or after a failed batch). The matcher maps it back into an engine
// order; base-denominated remaining quantity is derived from side + remaining amounts.
type OpenOrderHydration struct {
	OrderID             uuid.UUID
	UserID              uuid.UUID
	ClientOrderID       *string
	Side                string
	Price               uint64
	Type                string
	TimeInForce         string
	RemainingHaveAmount uint64
	RemainingWantAmount uint64
	ExpiresAt           *int64
}

// LoadOpenOrders returns every resting order for a market, ordered by open_orders.id
// (a BIGSERIAL), which reproduces insertion order and therefore per-price-level FIFO
// priority when the caller reinserts them into the book.
func (o *OrderRepository) LoadOpenOrders(ctx context.Context, marketID int) ([]OpenOrderHydration, error) {
	const q = `
		SELECT oo.order_id, ord.user_id, ord.client_order_id, oo.side, oo.price,
		       ord.type, ord.time_in_force, oo.remaining_have_amount, oo.remaining_want_amount,
		       ord.expires_at
		FROM open_orders oo
		JOIN orders ord ON ord.id = oo.order_id
		WHERE oo.market_id = $1
		ORDER BY oo.id ASC
	`
	rows, err := o.psql.QueryContext(ctx, q, marketID)
	if err != nil {
		o.logger.Error("LoadOpenOrders: " + err.Error())
		return nil, fmt.Errorf("load open orders: %w", err)
	}
	defer rows.Close()

	var out []OpenOrderHydration
	for rows.Next() {
		var h OpenOrderHydration
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&h.OrderID,
			&h.UserID,
			&h.ClientOrderID,
			&h.Side,
			&h.Price,
			&h.Type,
			&h.TimeInForce,
			&h.RemainingHaveAmount,
			&h.RemainingWantAmount,
			&expiresAt,
		); err != nil {
			o.logger.Error("LoadOpenOrders: scan: " + err.Error())
			return nil, fmt.Errorf("load open orders: scan: %w", err)
		}
		if expiresAt.Valid {
			v := expiresAt.Time.Unix()
			h.ExpiresAt = &v
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		o.logger.Error("LoadOpenOrders: " + err.Error())
		return nil, fmt.Errorf("load open orders: %w", err)
	}
	return out, nil
}

// --- small SQL-building helpers ---

// valuesPlaceholders builds "($1,$2,...),($n,...)" for rows×cols parameters with a
// single running index, for multi-row INSERT ... VALUES. Column types are inferred
// from the target table, so no casts are needed.
func valuesPlaceholders(rows, cols int) string {
	var b strings.Builder
	n := 1
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < cols; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d", n)
			n++
		}
		b.WriteByte(')')
	}
	return b.String()
}

// valuesPlaceholdersCast is like valuesPlaceholders but casts every placeholder to the
// given column type. A standalone VALUES list (used in UPDATE ... FROM) has no target
// table to infer types from, so the casts are required.
func valuesPlaceholdersCast(rows int, casts []string) string {
	cols := len(casts)
	var b strings.Builder
	n := 1
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < cols; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d::%s", n, casts[c])
			n++
		}
		b.WriteByte(')')
	}
	return b.String()
}

func uuidStrings(ids []uuid.UUID) []string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return s
}

func nullVal(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullU64(v *uint64) any {
	if v == nil {
		return nil
	}
	return int64(*v)
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func derefU64(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}
