package markets

import (
	"github.com/gofiber/fiber/v3"
)

func RegisterMarketRoutes(app fiber.Router, marketHandler *MarketHandler) {
	marketGroup := app.Group("/markets")
	marketGroup.Get("/", marketHandler.GetMarkets)
	marketGroup.Get("/prices", marketHandler.GetPrices)
	marketGroup.Get("/:market", marketHandler.GetMarket)
}
