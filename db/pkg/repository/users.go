package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
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
)

const UserUniqueConstraintName = "users_username_uk"

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
}

func (r *UserRepository) GetUserBalances(ctx context.Context, userID uuid.UUID) ([]UserBalance, error) {
	query := `
		SELECT
			i.name,
			i.symbol,
			i.decimals,
			ub.balance,
			ub.blocked
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
		if err := rows.Scan(&b.InstrumentName, &b.InstrumentSymbol, &b.InstrumentDecimals, &b.Balance, &b.Blocked); err != nil {
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

func (r *UserRepository) AddUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64) error {
	query := `
		INSERT INTO user_balances (user_id, instrument_id, balance)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, instrument_id)
		DO UPDATE SET balance = user_balances.balance + EXCLUDED.balance
	`
	_, err := r.psql.ExecContext(ctx, query, userID, instrumentID, amount)
	if err != nil {
		r.logger.Error("error adding user balance")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", error_prefix, ErrUpdateBalanceFailed)
	}
	return nil
}

func (r *UserRepository) RemoveUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64) error {
	query := `
		UPDATE user_balances
		SET balance = balance - $3
		WHERE user_id = $1 AND instrument_id = $2 AND balance >= $3
	`
	result, err := r.psql.ExecContext(ctx, query, userID, instrumentID, amount)
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
	return nil
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
