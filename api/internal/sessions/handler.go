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

const maxUserAgentLength = 512

// CredentialValidator is satisfied by users.UserService without importing that package.
type CredentialValidator interface {
	ValidateCredentials(ctx context.Context, username, password string) (uuid.UUID, error)
}

// No minimum length here on purpose: the password either matches the stored hash or it does
// not, and pinning login to whatever the registration rule happens to be today would lock out
// every account created under an older, shorter one. The cap stays, since argon2id burns CPU
// in proportion to the input it is handed.
type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required,max=128"`
}

type RevokeTokenHashRequest struct {
	TokenHash string `json:"session_id" validate:"required"`
}

type CreateTokenRequest struct {
	Scope string `json:"scope" validate:"required,oneof=read write"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type CreateTokenResponse struct {
	Token     string `json:"token"`
	Scope     string `json:"scope"`
	ExpiresAt int64  `json:"expires_at"`
}

type RefreshSessionResponse struct {
	ExpiresAt int64 `json:"expires_at"`
}

type GetActiveSessionResponse struct {
	CreatedAt int64 `json:"created_at"`
	ExpiresAt int64 `json:"expires_at"`
	// one-way hash of the bearer token, reused as the external session id — never accept for authentication
	SessionID string  `json:"session_id"`
	Origin    string  `json:"origin"`
	Scope     string  `json:"scope"`
	UserAgent *string `json:"user_agent,omitempty"`
	IPAddress *string `json:"ip_address,omitempty"`
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

	userAgent := utils.NilIfEmpty(utils.Truncate(c.Get(fiber.HeaderUserAgent), maxUserAgentLength))
	ipAddress := utils.NilIfEmpty(c.IP())

	token, err := h.sessionService.CreateSession(c.Context(), userID, userAgent, ipAddress)
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(LoginResponse{Token: token})
}

func (h *SessionHandler) CreateToken(c fiber.Ctx) error {
	var req CreateTokenRequest
	if err := c.Bind().Body(&req); err != nil {
		h.logger.Error("CreateToken: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	userID := middleware.UserIDFromContext(c)
	userAgent := utils.NilIfEmpty(utils.Truncate(c.Get(fiber.HeaderUserAgent), maxUserAgentLength))
	ipAddress := utils.NilIfEmpty(c.IP())

	rawToken, expiresAt, err := h.sessionService.MintToken(c.Context(), userID, req.Scope, userAgent, ipAddress)
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusCreated).JSON(CreateTokenResponse{
		Token:     rawToken,
		Scope:     req.Scope,
		ExpiresAt: expiresAt.Unix(),
	})
}

func (h *SessionHandler) RefreshSession(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)
	rawToken := strings.TrimPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")

	expiresAt, err := h.sessionService.RefreshSession(c.Context(), userID, rawToken)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "session can no longer be refreshed, please log in again")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(RefreshSessionResponse{ExpiresAt: expiresAt.Unix()})
}

func (h *SessionHandler) GetSessions(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)

	activeSessions, err := h.sessionService.GetActiveSessions(c.Context(), userID)
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	var parsedActiveSessions []GetActiveSessionResponse

	for _, activeSession := range activeSessions {
		parsedActiveSessions = append(parsedActiveSessions, GetActiveSessionResponse{
			CreatedAt: activeSession.CreatedAt,
			ExpiresAt: activeSession.ExpiresAt,
			SessionID: activeSession.TokenHash,
			Origin:    activeSession.Origin,
			Scope:     activeSession.Scope,
			UserAgent: activeSession.UserAgent,
			IPAddress: activeSession.IPAddress,
		})
	}

	return c.Status(fiber.StatusOK).JSON(parsedActiveSessions)
}

func (h *SessionHandler) DeleteActiveSession(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)

	var tokenHashRequest RevokeTokenHashRequest
	if err := c.Bind().Body(&tokenHashRequest); err != nil {
		h.logger.Error("Delete active session: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	err := h.sessionService.RevokeTokenByTokenHash(c.Context(), userID, tokenHashRequest.TokenHash)

	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(nil)
		}

		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(nil)
}

func (h *SessionHandler) Logout(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)
	rawToken := strings.TrimPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	if err := h.sessionService.RevokeToken(c.Context(), userID, rawToken); err != nil {
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
