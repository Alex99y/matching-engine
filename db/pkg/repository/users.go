package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/google/uuid"
)

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
				return ErrUserAlreadyExists
			}
			r.logger.Error("unknown unique constraint violation when inserting user")
			r.logger.ErrorO(err)
			return ErrUnknownUserConstraint
		}

		r.logger.Error("error inserting user")
		r.logger.ErrorO(err)
		return ErrUserInsertFailed
	}

	rowsAffected, err := row.RowsAffected()
	if rowsAffected == 0 || err != nil {
		r.logger.Error("no rows affected when inserting user")
		if err != nil {
			r.logger.ErrorO(err)
		}
		return ErrUserNotInserted
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
			return nil, ErrUserNotFound
		}

		r.logger.Error("error scanning user")
		r.logger.ErrorO(err)
		return nil, ErrUserGetFailed
	}

	return user, nil
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
