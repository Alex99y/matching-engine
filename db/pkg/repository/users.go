package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	dbutils "github.com/alex99y/matching-engine/db/pkg/utils"
	"github.com/google/uuid"
)

const error_prefix = "user repository:"

type UserRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

var (
	ErrUserAlreadyExists     = errors.New("user already exists")
	ErrUserInsertFailed      = errors.New("failed to insert user")
	ErrUserNotInserted       = errors.New("user not inserted")
	ErrUserNotFound          = errors.New("user not found")
	ErrUserGetFailed         = errors.New("failed to get user")
	ErrUnknownUserConstraint = errors.New("unknown user constraint violation")
	ErrGetBalancesFailed     = errors.New("failed to get user balances")
	ErrUpdateBalanceFailed   = errors.New("failed to update user balance")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrInsufficientFrozen    = errors.New("insufficient frozen balance")
	ErrOperationTxBegin      = errors.New("begin user operation transaction failed")
	ErrOperationTxCommit     = errors.New("commit user operation transaction failed")
	ErrOperationInsertFailed = errors.New("failed to insert user operation")
	ErrGetOperationsFailed   = errors.New("failed to get user operations")
	ErrSetFrozenFailed       = errors.New("failed to set user frozen status")
)

const UserUniqueConstraintName = "users_username_uk"

const (
	OperationTypeDeposit  = "deposit"
	OperationTypeWithdraw = "withdraw"
	OperationTypeFreeze   = "freeze"
	OperationTypeUnfreeze = "unfreeze"
)

func (r *UserRepository) InsertUser(
	ctx context.Context,
	username string,
	email string,
	passwordHash string,
) error {

	query := `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
	`

	row, err := r.psql.ExecContext(ctx, query, username, email, passwordHash)
	if err != nil {
		if constraint, isUnique := postgres.IsUniqueConstraintViolation(err); isUnique {
			if constraint == UserUniqueConstraintName {
				return fmt.Errorf("%s %w", error_prefix, ErrUserAlreadyExists)
			}
			r.logger.Error("unknown unique constraint violation when inserting user")
			r.logger.ErrorO(err)
			return fmt.Errorf("%s %w", error_prefix, ErrUnknownUserConstraint)
		}

		r.logger.Error("error inserting user")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUserInsertFailed)
	}

	rowsAffected, err := row.RowsAffected()
	if rowsAffected == 0 || err != nil {
		r.logger.Error("no rows affected when inserting user")
		if err != nil {
			r.logger.ErrorO(err)
		}
		return fmt.Errorf("%s %w", error_prefix, ErrUserNotInserted)
	}
	return nil
}

type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
}

func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := `
		SELECT id, username, email, password_hash
		FROM users
		WHERE username = $1
	`
	row := r.psql.QueryRowContext(ctx, query, username)
	user := &User{}

	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%s %w", error_prefix, ErrUserNotFound)
		}

		r.logger.Error("error scanning user")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", error_prefix, ErrUserGetFailed)
	}

	return user, nil
}

type UserBalance struct {
	InstrumentName     string
	InstrumentSymbol   string
	InstrumentDecimals int
	Balance            int64
	Blocked            int64
	Frozen             int64
}

func (r *UserRepository) GetUserBalances(ctx context.Context, userID uuid.UUID) ([]UserBalance, error) {
	query := `
		SELECT
			i.name,
			i.symbol,
			i.decimals,
			ub.balance,
			ub.blocked,
			ub.frozen
		FROM user_balances ub
		JOIN instruments i ON i.id = ub.instrument_id
		WHERE ub.user_id = $1
	`

	rows, err := r.psql.QueryContext(ctx, query, userID)
	if err != nil {
		r.logger.Error("error getting user balances")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", error_prefix, ErrGetBalancesFailed)
	}
	defer rows.Close()

	var balances []UserBalance
	for rows.Next() {
		var b UserBalance
		if err := rows.Scan(&b.InstrumentName, &b.InstrumentSymbol, &b.InstrumentDecimals, &b.Balance, &b.Blocked, &b.Frozen); err != nil {
			r.logger.Error("error scanning user balance")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("%s %w", error_prefix, ErrGetBalancesFailed)
		}
		balances = append(balances, b)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating user balances")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", error_prefix, ErrGetBalancesFailed)
	}

	return balances, nil
}

func insertUserOperation(
	ctx context.Context,
	tx *sql.Tx,
	userID uuid.UUID,
	instrumentID int,
	amount int64,
	opType string,
	reason *string,
) error {
	const query = `
		INSERT INTO user_operations (user_id, instrument_id, amount, type, reason)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := tx.ExecContext(ctx, query, userID, instrumentID, amount, opType, reason); err != nil {
		return fmt.Errorf("insert user operation: %w", err)
	}
	return nil
}

func (r *UserRepository) AddUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	tx, rollback, err := dbutils.BeginTx(ctx, r.psql, r.logger, "AddUserBalance")
	if err != nil {
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxBegin, err)
	}
	defer rollback()

	const query = `
		INSERT INTO user_balances (user_id, instrument_id, balance)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, instrument_id)
		DO UPDATE SET balance = user_balances.balance + EXCLUDED.balance
	`
	if _, err := tx.ExecContext(ctx, query, userID, instrumentID, amount); err != nil {
		r.logger.Error("error adding user balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}

	if err := insertUserOperation(ctx, tx, userID, instrumentID, amount, OperationTypeDeposit, reason); err != nil {
		r.logger.Error("error inserting deposit operation")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrOperationInsertFailed)
	}

	if err := tx.Commit(); err != nil {
		r.logger.Error("AddUserBalance: commit: " + err.Error())
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxCommit, err)
	}
	return nil
}

func (r *UserRepository) RemoveUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	tx, rollback, err := dbutils.BeginTx(ctx, r.psql, r.logger, "RemoveUserBalance")
	if err != nil {
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxBegin, err)
	}
	defer rollback()

	const query = `
		UPDATE user_balances
		SET balance = balance - $3
		WHERE user_id = $1 AND instrument_id = $2 AND balance >= $3
	`
	result, err := tx.ExecContext(ctx, query, userID, instrumentID, amount)
	if err != nil {
		r.logger.Error("error removing user balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error checking rows affected when removing balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	if rows == 0 {
		return fmt.Errorf("%s %w", error_prefix, ErrInsufficientBalance)
	}

	if err := insertUserOperation(ctx, tx, userID, instrumentID, amount, OperationTypeWithdraw, reason); err != nil {
		r.logger.Error("error inserting withdraw operation")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrOperationInsertFailed)
	}

	if err := tx.Commit(); err != nil {
		r.logger.Error("RemoveUserBalance: commit: " + err.Error())
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxCommit, err)
	}
	return nil
}

func (r *UserRepository) FreezeUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	tx, rollback, err := dbutils.BeginTx(ctx, r.psql, r.logger, "FreezeUserBalance")
	if err != nil {
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxBegin, err)
	}
	defer rollback()

	const query = `
		UPDATE user_balances
		SET balance = balance - $3, frozen = frozen + $3
		WHERE user_id = $1 AND instrument_id = $2 AND balance >= $3
	`
	result, err := tx.ExecContext(ctx, query, userID, instrumentID, amount)
	if err != nil {
		r.logger.Error("error freezing user balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error checking rows affected when freezing balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	if rows == 0 {
		return fmt.Errorf("%s %w", error_prefix, ErrInsufficientBalance)
	}

	if err := insertUserOperation(ctx, tx, userID, instrumentID, amount, OperationTypeFreeze, reason); err != nil {
		r.logger.Error("error inserting freeze operation")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrOperationInsertFailed)
	}

	if err := tx.Commit(); err != nil {
		r.logger.Error("FreezeUserBalance: commit: " + err.Error())
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxCommit, err)
	}
	return nil
}

func (r *UserRepository) UnfreezeUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	tx, rollback, err := dbutils.BeginTx(ctx, r.psql, r.logger, "UnfreezeUserBalance")
	if err != nil {
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxBegin, err)
	}
	defer rollback()

	const query = `
		UPDATE user_balances
		SET balance = balance + $3, frozen = frozen - $3
		WHERE user_id = $1 AND instrument_id = $2 AND frozen >= $3
	`
	result, err := tx.ExecContext(ctx, query, userID, instrumentID, amount)
	if err != nil {
		r.logger.Error("error unfreezing user balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error checking rows affected when unfreezing balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	if rows == 0 {
		return fmt.Errorf("%s %w", error_prefix, ErrInsufficientFrozen)
	}

	if err := insertUserOperation(ctx, tx, userID, instrumentID, amount, OperationTypeUnfreeze, reason); err != nil {
		r.logger.Error("error inserting unfreeze operation")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrOperationInsertFailed)
	}

	if err := tx.Commit(); err != nil {
		r.logger.Error("UnfreezeUserBalance: commit: " + err.Error())
		return fmt.Errorf("%s %w: %w", error_prefix, ErrOperationTxCommit, err)
	}
	return nil
}

func (r *UserRepository) FreezeUser(ctx context.Context, userID uuid.UUID) error {
	return r.setUserFrozen(ctx, userID, true)
}

func (r *UserRepository) UnfreezeUser(ctx context.Context, userID uuid.UUID) error {
	return r.setUserFrozen(ctx, userID, false)
}

func (r *UserRepository) setUserFrozen(ctx context.Context, userID uuid.UUID, frozen bool) error {
	const query = `UPDATE users SET frozen = $2 WHERE id = $1`
	result, err := r.psql.ExecContext(ctx, query, userID, frozen)
	if err != nil {
		r.logger.Error("error setting user frozen status")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrSetFrozenFailed)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error checking rows affected when setting user frozen status")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrSetFrozenFailed)
	}
	if rows == 0 {
		return fmt.Errorf("%s %w", error_prefix, ErrUserNotFound)
	}
	return nil
}

type UserOperation struct {
	ID                 uuid.UUID
	InstrumentName     string
	InstrumentSymbol   string
	InstrumentDecimals int
	Amount             int64
	Type               string
	Reason             *string
	CreatedAt          int64
}

func (r *UserRepository) GetUserOperations(
	ctx context.Context,
	userID uuid.UUID,
	startDate, endDate *time.Time,
	limit int,
) ([]UserOperation, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT
			uo.id,
			i.name,
			i.symbol,
			i.decimals,
			uo.amount,
			uo.type,
			uo.reason,
			uo.created_at
		FROM user_operations uo
		JOIN instruments i ON i.id = uo.instrument_id
		WHERE uo.user_id = $1`)
	args := []any{userID}

	if startDate != nil {
		args = append(args, *startDate)
		sb.WriteString(fmt.Sprintf("\nAND uo.created_at >= $%d", len(args)))
	}
	if endDate != nil {
		args = append(args, *endDate)
		sb.WriteString(fmt.Sprintf("\nAND uo.created_at < $%d", len(args)))
	}

	args = append(args, limit)
	sb.WriteString(fmt.Sprintf("\nORDER BY uo.created_at DESC\nLIMIT $%d", len(args)))

	rows, err := r.psql.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		r.logger.Error("error getting user operations")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", error_prefix, ErrGetOperationsFailed)
	}
	defer rows.Close()

	var operations []UserOperation
	for rows.Next() {
		var op UserOperation
		var createdAt time.Time
		if err := rows.Scan(
			&op.ID, &op.InstrumentName, &op.InstrumentSymbol, &op.InstrumentDecimals,
			&op.Amount, &op.Type, &op.Reason, &createdAt,
		); err != nil {
			r.logger.Error("error scanning user operation")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("%s %w", error_prefix, ErrGetOperationsFailed)
		}
		op.CreatedAt = createdAt.Unix()
		operations = append(operations, op)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating user operations")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", error_prefix, ErrGetOperationsFailed)
	}

	return operations, nil
}

func NewUserRepository(logger *logger.Logger, psql *sql.DB) *UserRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &UserRepository{
		psql:   psql,
		logger: logger,
	}
}
