package faucet

import (
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/gofiber/fiber/v3"
)

func RegisterFaucetRoutes(app fiber.Router, authMiddleware middleware.AuthMiddleware, faucetHandler *FaucetHandler) {
	auth := fiber.Handler(authMiddleware)
	app.Post(
		"/faucet",
		auth,
		middleware.RequireWrite,
		faucetHandler.RequestFunds,
	)
}
