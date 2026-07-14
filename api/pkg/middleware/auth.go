package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type AuthMiddleware fiber.Handler

type contextKey string

const contextKeyUserID contextKey = "user_id"

var (
	// ErrInvalidSession is returned by TokenValidator when the token is missing, expired, or revoked.
	// Defined here so both the sessions service and this middleware can reference it without cycles.
	ErrInvalidSession = errors.New("invalid or expired session")

	// ErrInvalidCredentials is returned by CredentialValidator when username or password is wrong.
	// Defined here so both the users service and the sessions handler can reference it without cycles.
	ErrInvalidCredentials = errors.New("invalid username or password")
)

// TokenValidator is satisfied by sessions.SessionService without importing that package.
type TokenValidator interface {
	ValidateToken(ctx context.Context, rawToken string) (*uuid.UUID, error)
}

func Auth(log *logger.Logger, validator TokenValidator) AuthMiddleware {
	return func(c fiber.Ctx) error {
		authHeader := c.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "missing or invalid authorization header")
		}

		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		userID, err := validator.ValidateToken(c.Context(), rawToken)
		if err != nil {
			if errors.Is(err, ErrInvalidSession) {
				return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "invalid or expired session")
			}
			return utils.NewServerErrorResponse(c, log, err)
		}

		c.Locals(contextKeyUserID, *userID)
		return c.Next()
	}
}

// UserIDFromContext returns the authenticated user's ID stored by the Auth middleware.
func UserIDFromContext(c fiber.Ctx) uuid.UUID {
	id, _ := c.Locals(contextKeyUserID).(uuid.UUID)
	return id
}
