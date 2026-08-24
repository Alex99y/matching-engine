package matches

import "github.com/gofiber/fiber/v3"

func RegisterMatchRoutes(app fiber.Router, handler *MatchHandler) {
	app.Get("/markets/:market/matches", handler.GetMatches)
}
