package orders

import (
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/gofiber/fiber/v3"
)

func RegisterOrderRoutes(app fiber.Router, authMiddleware middleware.AuthMiddleware, orderHandler *OrderHandler) {
	userGroup := app.Group("/order")
	userGroup.Post(
		"/",
		validations.ValidateContentType(validations.ContentTypeJSON),
		authMiddleware,
		orderHandler.CreateOrder,
	)
	userGroup.Get(
		"/",
		authMiddleware,
		orderHandler.GetOrders,
	)
	userGroup.Get(
		"/:id",
		authMiddleware,
		orderHandler.GetOrder,
	)
}
