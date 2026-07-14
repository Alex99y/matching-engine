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
}
