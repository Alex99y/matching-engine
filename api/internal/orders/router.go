package orders

import (
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/gofiber/fiber/v3"
)

func RegisterOrderRoutes(app fiber.Router, authMiddleware middleware.AuthMiddleware, orderHandler *OrderHandler) {
	auth := fiber.Handler(authMiddleware)
	userGroup := app.Group("/order")
	userGroup.Post(
		"/",
		validations.ValidateContentType(validations.ContentTypeJSON),
		auth,
		orderHandler.CreateOrder,
	)
	userGroup.Get(
		"/",
		auth,
		orderHandler.GetOrders,
	)
	userGroup.Get(
		"/:id",
		auth,
		orderHandler.GetOrder,
	)
	userGroup.Delete(
		"/:id",
		auth,
		orderHandler.CancelOrder,
	)
}
