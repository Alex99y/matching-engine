package users

import (
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/gofiber/fiber/v3"
)

func RegisterUserRoutes(app fiber.Router, authMiddleware middleware.AuthMiddleware, userHandler *UserHandler) {
	auth := fiber.Handler(authMiddleware)
	userGroup := app.Group("/users")
	userGroup.Post(
		"/register",
		validations.ValidateContentType(validations.ContentTypeJSON),
		userHandler.CreateUser,
	)
	userGroup.Post(
		"/check-username",
		validations.ValidateContentType(validations.ContentTypeJSON),
		userHandler.IsUsernameAvailable,
	)
	userGroup.Get(
		"/balances",
		auth,
		userHandler.GetBalance,
	)
}
