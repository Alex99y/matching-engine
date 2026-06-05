package instruments

import (
	"github.com/gofiber/fiber/v3"
)

func RegisterInstrumentRoutes(app fiber.Router, instrumentHandler *InstrumentHandler) {
	instrumentGroup := app.Group("/instruments")
	instrumentGroup.Get("/", instrumentHandler.GetInstruments)
	instrumentGroup.Get("/:symbol", instrumentHandler.GetInstrument)
}
