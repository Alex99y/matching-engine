package sessions

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/token"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

const sessionTTL = 7 * 24 * time.Hour

var (
	ErrCreateSession     = errors.New("could not create session")
	ErrRevokeSession     = errors.New("could not revoke session")
	ErrGetActiveSessions = errors.New("could not get active sessions")
	ErrSessionNotFound   = errors.New("session not found")
)

type SessionRepository interface {
	InsertSession(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	GetActiveSessionByTokenHash(ctx context.Context, tokenHash string) (*repository.Session, error)
	RevokeSessionByTokenHash(ctx context.Context, userID uuid.UUID, tokenHash string) error
	GetActiveSessionByUserId(ctx context.Context, userID uuid.UUID) ([]repository.Session, error)
}

type ActiveSessions struct {
	CreatedAt int64
	ExpiresAt int64
	TokenHash string
}

type SessionService struct {
	logger     *logger.Logger
	repository SessionRepository
}

func (s *SessionService) CreateSession(ctx context.Context, userID uuid.UUID) (string, error) {
	rawToken, tokenHash, err := token.Generate()
	if err != nil {
		s.logger.ErrorO(err)
		return "", ErrCreateSession
	}

	expiresAt := time.Now().Add(sessionTTL)
	if err := s.repository.InsertSession(ctx, userID, tokenHash, expiresAt); err != nil {
		return "", ErrCreateSession
	}

	return rawToken, nil
}

// ValidateToken hashes rawToken and looks up an active session. Returns middleware.ErrInvalidSession
// when no active session exists so the Auth middleware can map it to a 401 without importing this package.
func (s *SessionService) ValidateToken(ctx context.Context, rawToken string) (*uuid.UUID, error) {
	tokenHash := token.Hash(rawToken)
	session, err := s.repository.GetActiveSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return nil, middleware.ErrInvalidSession
		}
		s.logger.ErrorO(err)
		return nil, err
	}
	return &session.UserID, nil
}

func (s *SessionService) RevokeTokenByTokenHash(ctx context.Context, userID uuid.UUID, tokenHash string) error {
	if err := s.repository.RevokeSessionByTokenHash(ctx, userID, tokenHash); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return ErrRevokeSession
	}
	return nil
}

func (s *SessionService) RevokeToken(ctx context.Context, userID uuid.UUID, rawToken string) error {
	tokenHash := token.Hash(rawToken)
	return s.RevokeTokenByTokenHash(ctx, userID, tokenHash)
}

func (s *SessionService) GetActiveSessions(ctx context.Context, userID uuid.UUID) ([]ActiveSessions, error) {
	sessions, err := s.repository.GetActiveSessionByUserId(ctx, userID)
	if err != nil {
		return nil, ErrGetActiveSessions
	}

	activeSessions := make([]ActiveSessions, len(sessions))
	for i, session := range sessions {
		activeSessions[i] = ActiveSessions{
			CreatedAt: session.CreatedAt.Unix(),
			ExpiresAt: session.ExpiresAt.Unix(),
			TokenHash: session.TokenHash,
		}
	}

	return activeSessions, nil
}

func NewSessionService(logger *logger.Logger, repo SessionRepository) *SessionService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if repo == nil {
		panic("session repository cannot be nil")
	}
	return &SessionService{logger: logger, repository: repo}
}
