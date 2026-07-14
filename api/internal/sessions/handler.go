package sessions

import (
	"context"
	"errors"
	"strings"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/google/uuid"
)

// CredentialValidator is satisfied by users.UserService without importing that package.
type CredentialValidator interface {
	ValidateCredentials(ctx context.Context, username, password string) (uuid.UUID, error)
}

type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required,min=10"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type SessionHandler struct {
	logger         *logger.Logger
	sessionService *SessionService
	credValidator  CredentialValidator
}

func (h *SessionHandler) Login(c fiber.Ctx) error {
	var req LoginRequest
	if err := c.Bind().Body(&req); err != nil {
		h.logger.Error("Login: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	userID, err := h.credValidator.ValidateCredentials(c.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, middleware.ErrInvalidCredentials) {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "invalid username or password")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	token, err := h.sessionService.CreateSession(c.Context(), userID)
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(LoginResponse{Token: token})
}

func (h *SessionHandler) Logout(c fiber.Ctx) error {
	rawToken := strings.TrimPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	if err := h.sessionService.RevokeToken(c.Context(), rawToken); err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func NewSessionHandler(
	logger *logger.Logger,
	sessionService *SessionService,
	credValidator CredentialValidator,
) *SessionHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if sessionService == nil {
		panic("session service cannot be nil")
	}
	if credValidator == nil {
		panic("credential validator cannot be nil")
	}
	return &SessionHandler{
		logger:         logger,
		sessionService: sessionService,
		credValidator:  credValidator,
	}
}
