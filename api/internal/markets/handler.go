package markets

import (
	"errors"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

type MarketHandler struct {
	logger        *logger.Logger
	marketService *MarketService
}

type GetMarketResponse struct {
	BaseSymbol    string `json:"base_symbol"`
	QuoteSymbol   string `json:"quote_symbol"`
	PriceQuantum  int64  `json:"price_quantum"`
	AmountQuantum int64  `json:"amount_quantum"`
	MinOrderSize  int64  `json:"min_order_size"`
	MaxOrderSize  int64  `json:"max_order_size"`
}

func (h *MarketHandler) GetMarket(c fiber.Ctx) error {
	marketRef := c.Params("market")

	market, err := h.marketService.GetMarket(c.Context(), marketRef)
	if err != nil {
		if errors.Is(err, ErrMarketNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "market not found")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(GetMarketResponse{
		BaseSymbol:    market.BaseSymbol,
		QuoteSymbol:   market.QuoteSymbol,
		PriceQuantum:  market.PriceQuantum,
		AmountQuantum: market.AmountQuantum,
		MinOrderSize:  market.MinOrderSize,
		MaxOrderSize:  market.MaxOrderSize,
	})
}

func (h *MarketHandler) GetMarkets(c fiber.Ctx) error {
	markets, err := h.marketService.GetMarkets(c.Context())
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	response := make([]GetMarketResponse, len(markets))
	for i, m := range markets {
		response[i] = GetMarketResponse{
			BaseSymbol:    m.BaseSymbol,
			QuoteSymbol:   m.QuoteSymbol,
			PriceQuantum:  m.PriceQuantum,
			AmountQuantum: m.AmountQuantum,
			MinOrderSize:  m.MinOrderSize,
			MaxOrderSize:  m.MaxOrderSize,
		}
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func NewMarketHandler(logger *logger.Logger, marketService *MarketService) *MarketHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if marketService == nil {
		panic("market service cannot be nil")
	}
	return &MarketHandler{
		logger:        logger,
		marketService: marketService,
	}
}
