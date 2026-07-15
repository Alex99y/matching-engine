package candles

import "github.com/gofiber/fiber/v3"

func RegisterCandleRoutes(app fiber.Router, handler *CandleHandler) {
	app.Get("/markets/:market/candles", handler.GetCandles)
}
