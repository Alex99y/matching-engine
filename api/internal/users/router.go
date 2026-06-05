package users

import (
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/gofiber/fiber/v3"
)

func RegisterUserRoutes(app fiber.Router, userHandler *UserHandler) {
	userGroup := app.Group("/users")
	userGroup.Post(
		"/register",
		validations.ValidateContentType(validations.ContentTypeJSON),
		userHandler.CreateUser,
	)
	userGroup.Post(
		"/login",
		validations.ValidateContentType(validations.ContentTypeJSON),
		userHandler.LoginUser,
	)
	userGroup.Post(
		"/check-username",
		validations.ValidateContentType(validations.ContentTypeJSON),
		userHandler.IsUsernameAvailable,
	)
}
