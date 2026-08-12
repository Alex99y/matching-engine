package openapi

import "github.com/gofiber/fiber/v3"

func RegisterOpenAPIRoutes(app fiber.Router) {
	app.Get("/openapi.yaml", GetSpec)
}
