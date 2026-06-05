package instruments

import (
	"errors"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

type InstrumentHandler struct {
	logger            *logger.Logger
	instrumentService *InstrumentService
}

type GetInstrumentResponse struct {
	Name      string    `json:"name"`
	Symbol    string    `json:"symbol"`
	Decimals  int       `json:"decimals"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *InstrumentHandler) GetInstrument(c fiber.Ctx) error {
	symbol := c.Params("symbol")

	instrument, err := h.instrumentService.GetInstrument(c.Context(), symbol)
	if err != nil {
		if errors.Is(err, ErrInstrumentNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "instrument not found")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(GetInstrumentResponse{
		Name:      instrument.Name,
		Symbol:    instrument.Symbol,
		Decimals:  instrument.Decimals,
		CreatedAt: instrument.CreatedAt,
	})
}

func (h *InstrumentHandler) GetInstruments(c fiber.Ctx) error {
	instruments, err := h.instrumentService.GetInstruments(c.Context())
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	response := make([]GetInstrumentResponse, len(instruments))
	for i, inst := range instruments {
		response[i] = GetInstrumentResponse{
			Name:      inst.Name,
			Symbol:    inst.Symbol,
			Decimals:  inst.Decimals,
			CreatedAt: inst.CreatedAt,
		}
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func NewInstrumentHandler(logger *logger.Logger, instrumentService *InstrumentService) *InstrumentHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if instrumentService == nil {
		panic("instrument service cannot be nil")
	}
	return &InstrumentHandler{
		logger:            logger,
		instrumentService: instrumentService,
	}
}
