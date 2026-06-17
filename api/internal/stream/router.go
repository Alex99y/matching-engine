package stream

import (
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/gofiber/fiber/v3"
)

// RegisterStreamRoutes mounts the SSE market-data stream. The connection is authenticated so the
// user id is available for the private order stream (C2); C1 serves public book + trades.
func RegisterStreamRoutes(
	app fiber.Router,
	authMiddleware middleware.AuthMiddleware,
	handler *StreamHandler,
) {
	// Register the static route before the :market param route so "users" is not captured as a market.
	auth := fiber.Handler(authMiddleware)
	app.Get("/stream/users", auth, handler.UserStream)
	app.Get("/stream/:market", handler.MarketStream)
}
