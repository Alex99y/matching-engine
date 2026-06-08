package middleware

import (
	"errors"
	"strings"

	pkgjwt "github.com/alex99y/matching-engine/api/pkg/jwt"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

var ErrInvalidUserIDInToken = errors.New("invalid user_id in token")

type AuthMiddleware fiber.Handler

type contextKey string

const (
	contextKeyUserID    contextKey = "user_id"
	contextKeySessionID contextKey = "session_id"
)

func Auth(jwtManager *pkgjwt.Manager) AuthMiddleware {
	return func(c fiber.Ctx) error {
		authHeader := c.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "missing or invalid authorization header")
		}

		claims, err := jwtManager.Verify(strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			if errors.Is(err, pkgjwt.ErrExpiredToken) {
				return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "token has expired")
			}
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "invalid token")
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "invalid token")
		}

		c.Locals(contextKeyUserID, userID)
		c.Locals(contextKeySessionID, claims.SessionID)
		return c.Next()
	}
}

// UserIDFromContext returns the authenticated user's ID stored by the Auth middleware.
func UserIDFromContext(c fiber.Ctx) uuid.UUID {
	id, _ := c.Locals(contextKeyUserID).(uuid.UUID)
	return id
}

// SessionIDFromContext returns the session ID stored by the Auth middleware.
func SessionIDFromContext(c fiber.Ctx) string {
	id, _ := c.Locals(contextKeySessionID).(string)
	return id
}
