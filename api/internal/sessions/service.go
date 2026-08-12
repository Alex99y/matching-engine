package sessions

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/sessionscope"
	"github.com/alex99y/matching-engine/common/pkg/token"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

const (
	sessionTTL    = 7 * 24 * time.Hour
	maxSessionAge = 30 * 24 * time.Hour
)

var (
	ErrCreateSession     = errors.New("could not create session")
	ErrRevokeSession     = errors.New("could not revoke session")
	ErrGetActiveSessions = errors.New("could not get active sessions")
	ErrSessionNotFound   = errors.New("session not found")
	ErrRefreshSession    = errors.New("could not refresh session")
)

type SessionRepository interface {
	InsertSession(ctx context.Context, params repository.InsertSessionParams) error
	GetActiveSessionByTokenHash(ctx context.Context, tokenHash string) (*repository.Session, error)
	RevokeSessionByTokenHash(ctx context.Context, userID uuid.UUID, tokenHash string) error
	GetActiveSessionByUserId(ctx context.Context, userID uuid.UUID) ([]repository.Session, error)
	RefreshSession(ctx context.Context, userID uuid.UUID, tokenHash string, newExpiresAt, minCreatedAt time.Time) error
}

type ActiveSessions struct {
	CreatedAt int64
	ExpiresAt int64
	TokenHash string
	Origin    string
	Scope     string
	UserAgent *string
	IPAddress *string
}

type SessionService struct {
	logger     *logger.Logger
	repository SessionRepository
}

func (s *SessionService) CreateSession(ctx context.Context, userID uuid.UUID, userAgent, ipAddress *string) (string, error) {
	rawToken, _, err := s.createSession(ctx, userID, sessionscope.OriginLogin, sessionscope.ScopeWrite, userAgent, ipAddress)
	return rawToken, err
}

func (s *SessionService) MintToken(ctx context.Context, userID uuid.UUID, scope string, userAgent, ipAddress *string) (string, time.Time, error) {
	return s.createSession(ctx, userID, sessionscope.OriginMinted, scope, userAgent, ipAddress)
}

func (s *SessionService) createSession(
	ctx context.Context,
	userID uuid.UUID,
	origin, scope string,
	userAgent, ipAddress *string,
) (string, time.Time, error) {
	rawToken, tokenHash, err := token.Generate()
	if err != nil {
		s.logger.ErrorO(err)
		return "", time.Time{}, ErrCreateSession
	}

	expiresAt := time.Now().Add(sessionTTL)
	params := repository.InsertSessionParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		Origin:    origin,
		Scope:     scope,
		UserAgent: userAgent,
		IPAddress: ipAddress,
	}
	if err := s.repository.InsertSession(ctx, params); err != nil {
		return "", time.Time{}, ErrCreateSession
	}

	return rawToken, expiresAt, nil
}

func (s *SessionService) ValidateToken(ctx context.Context, rawToken string) (*middleware.SessionInfo, error) {
	tokenHash := token.Hash(rawToken)
	session, err := s.repository.GetActiveSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return nil, middleware.ErrInvalidSession
		}
		s.logger.ErrorO(err)
		return nil, err
	}
	return &middleware.SessionInfo{
		UserID: session.UserID,
		Origin: session.Origin,
		Scope:  session.Scope,
	}, nil
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

func (s *SessionService) RefreshSession(ctx context.Context, userID uuid.UUID, rawToken string) (time.Time, error) {
	tokenHash := token.Hash(rawToken)
	newExpiresAt := time.Now().Add(sessionTTL)
	minCreatedAt := time.Now().Add(-maxSessionAge)

	if err := s.repository.RefreshSession(ctx, userID, tokenHash, newExpiresAt, minCreatedAt); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return time.Time{}, ErrSessionNotFound
		}
		return time.Time{}, ErrRefreshSession
	}

	return newExpiresAt, nil
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
			Origin:    session.Origin,
			Scope:     session.Scope,
			UserAgent: session.UserAgent,
			IPAddress: session.IPAddress,
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
