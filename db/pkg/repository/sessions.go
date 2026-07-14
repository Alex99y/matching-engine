package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/google/uuid"
)

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionInsertFailed = errors.New("failed to insert session")
	ErrSessionRevokeFailed = errors.New("failed to revoke session")
	ErrSessionGetFailed   = errors.New("failed to get sessions")
)

type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type SessionRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

func (r *SessionRepository) InsertSession(
	ctx context.Context,
	userID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) error {
	query := `
		INSERT INTO sessions (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`
	_, err := r.psql.ExecContext(ctx, query, userID, tokenHash, expiresAt)
	if err != nil {
		r.logger.Error("error inserting session")
		r.logger.ErrorO(err)
		return fmt.Errorf("session repository: %w", ErrSessionInsertFailed)
	}
	return nil
}

func (r *SessionRepository) GetActiveSessionByTokenHash(
	ctx context.Context,
	tokenHash string,
) (*Session, error) {
	query := `
		SELECT id, user_id, token_hash, created_at, expires_at, revoked_at
		FROM sessions
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
	`
	row := r.psql.QueryRowContext(ctx, query, tokenHash)
	s := &Session{}
	err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session repository: %w", ErrSessionNotFound)
		}
		r.logger.Error("error scanning session")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("session repository: %w", ErrSessionGetFailed)
	}
	return s, nil
}

func (r *SessionRepository) RevokeSessionByTokenHash(
	ctx context.Context,
	tokenHash string,
) error {
	query := `
		UPDATE sessions
		SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`
	_, err := r.psql.ExecContext(ctx, query, tokenHash)
	if err != nil {
		r.logger.Error("error revoking session")
		r.logger.ErrorO(err)
		return fmt.Errorf("session repository: %w", ErrSessionRevokeFailed)
	}
	return nil
}

func (r *SessionRepository) GetSessionsByUserID(
	ctx context.Context,
	userID uuid.UUID,
) ([]Session, error) {
	query := `
		SELECT id, user_id, token_hash, created_at, expires_at, revoked_at
		FROM sessions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.psql.QueryContext(ctx, query, userID)
	if err != nil {
		r.logger.Error("error querying sessions by user")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("session repository: %w", ErrSessionGetFailed)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt); err != nil {
			r.logger.Error("error scanning session row")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("session repository: %w", ErrSessionGetFailed)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating session rows")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("session repository: %w", ErrSessionGetFailed)
	}
	return sessions, nil
}

func NewSessionRepository(logger *logger.Logger, psql *sql.DB) *SessionRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &SessionRepository{psql: psql, logger: logger}
}
