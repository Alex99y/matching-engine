package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/sessionscope"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type AuthMiddleware fiber.Handler

type contextKey string

const (
	contextKeyUserID contextKey = "user_id"
	contextKeyOrigin contextKey = "session_origin"
	contextKeyScope  contextKey = "session_scope"
	contextKeyFrozen contextKey = "user_frozen"
)

var (
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

type SessionInfo struct {
	UserID uuid.UUID
	Origin string
	Scope  string
	Frozen bool
}

type TokenValidator interface {
	ValidateToken(ctx context.Context, rawToken string) (*SessionInfo, error)
}

func Auth(log *logger.Logger, validator TokenValidator) AuthMiddleware {
	return func(c fiber.Ctx) error {
		authHeader := c.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "missing or invalid authorization header")
		}

		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		info, err := validator.ValidateToken(c.Context(), rawToken)
		if err != nil {
			if errors.Is(err, ErrInvalidSession) {
				return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "invalid or expired session")
			}
			return utils.NewServerErrorResponse(c, log, err)
		}

		c.Locals(contextKeyUserID, info.UserID)
		c.Locals(contextKeyOrigin, info.Origin)
		c.Locals(contextKeyScope, info.Scope)
		c.Locals(contextKeyFrozen, info.Frozen)
		return c.Next()
	}
}

// UserIDFromContext returns the authenticated user's ID stored by the Auth middleware.
func UserIDFromContext(c fiber.Ctx) uuid.UUID {
	id, _ := c.Locals(contextKeyUserID).(uuid.UUID)
	return id
}

// SessionOriginFromContext returns the authenticated session's origin ("login"/"minted") stored
// by the Auth middleware.
func SessionOriginFromContext(c fiber.Ctx) string {
	origin, _ := c.Locals(contextKeyOrigin).(string)
	return origin
}

// SessionScopeFromContext returns the authenticated session's scope ("read"/"write") stored by
// the Auth middleware.
func SessionScopeFromContext(c fiber.Ctx) string {
	scope, _ := c.Locals(contextKeyScope).(string)
	return scope
}

// UserFrozenFromContext returns whether the authenticated user's account is frozen, stored by
// the Auth middleware.
func UserFrozenFromContext(c fiber.Ctx) bool {
	frozen, _ := c.Locals(contextKeyFrozen).(bool)
	return frozen
}

// RequireWrite must run after Auth. It rejects a read-scoped session with 403 so trading routes
// (order create/cancel) can never be reached by a read-only token.
func RequireWrite(c fiber.Ctx) error {
	if SessionScopeFromContext(c) != sessionscope.ScopeWrite {
		return utils.NewErrorResponse(c, fiber.StatusForbidden, "this action requires a write-scoped session")
	}
	return c.Next()
}

// RequireLoginOrigin must run after Auth. It rejects a minted token with 403 so only a
// password-authenticated login session can mint further tokens or revoke another session — a
// minted token is always a dead-end: it can never expand its own access or touch a session
// other than itself.
func RequireLoginOrigin(c fiber.Ctx) error {
	if SessionOriginFromContext(c) != sessionscope.OriginLogin {
		return utils.NewErrorResponse(c, fiber.StatusForbidden, "this action requires a login session")
	}
	return c.Next()
}

// RequireNotFrozen must run after Auth. It rejects a frozen account with 403 on fund-moving
// routes (order creation, faucet) — attach it per-route rather than globally, since a frozen
// account must still be able to cancel resting orders.
func RequireNotFrozen(c fiber.Ctx) error {
	if UserFrozenFromContext(c) {
		return utils.NewErrorResponse(c, fiber.StatusForbidden, "account is frozen")
	}
	return c.Next()
}
