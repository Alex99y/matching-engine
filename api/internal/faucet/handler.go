package faucet

import (
	"errors"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

type FaucetHandler struct {
	logger        *logger.Logger
	faucetService *FaucetService
}

type FaucetResponse struct {
	Symbol string `json:"symbol"`
	Amount int64  `json:"amount"`
}

func (h *FaucetHandler) RequestFunds(c fiber.Ctx) error {
	symbol := c.Query("instrument")
	if symbol == "" {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "instrument query param is required")
	}

	userID := middleware.UserIDFromContext(c)

	credit, err := h.faucetService.Request(c.Context(), userID, symbol)
	if err != nil {
		if errors.Is(err, ErrInstrumentNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "instrument not found")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(FaucetResponse{
		Symbol: credit.Symbol,
		Amount: credit.Amount,
	})
}

func NewFaucetHandler(logger *logger.Logger, faucetService *FaucetService) *FaucetHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if faucetService == nil {
		panic("faucet service cannot be nil")
	}
	return &FaucetHandler{logger: logger, faucetService: faucetService}
}
