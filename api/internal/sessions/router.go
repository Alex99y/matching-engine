package sessions

import (
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/gofiber/fiber/v3"
)

func RegisterSessionRoutes(app fiber.Router, authMiddleware middleware.AuthMiddleware, handler *SessionHandler) {
	auth := fiber.Handler(authMiddleware)
	sessGroup := app.Group("/sessions")
	sessGroup.Post(
		"",
		validations.ValidateContentType(validations.ContentTypeJSON),
		handler.Login,
	)
	sessGroup.Delete("", auth, handler.Logout)
	sessGroup.Post("/refresh", auth, handler.RefreshSession)
	sessGroup.Get("/active", auth, handler.GetSessions)
	sessGroup.Delete(
		"/active",
		validations.ValidateContentType(validations.ContentTypeJSON),
		auth,
		middleware.RequireLoginOrigin,
		handler.DeleteActiveSession,
	)
	// Minting a token requires a full login session — a minted token can never mint another.
	sessGroup.Post(
		"/tokens",
		validations.ValidateContentType(validations.ContentTypeJSON),
		auth,
		middleware.RequireLoginOrigin,
		handler.CreateToken,
	)
}
