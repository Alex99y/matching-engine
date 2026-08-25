package markets

import (
	"errors"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	commonutils "github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type MarketHandler struct {
	logger        *logger.Logger
	marketService *MarketService
	marketQuanta  map[string]uint64 // served market ref -> price_quantum (validates :market and ?group)
}

type GetMarketResponse struct {
	BaseSymbol    string `json:"base_symbol"`
	QuoteSymbol   string `json:"quote_symbol"`
	PriceQuantum  uint64 `json:"price_quantum"`
	AmountQuantum uint64 `json:"amount_quantum"`
	MinOrderSize  uint64 `json:"min_order_size"`
	MaxOrderSize  uint64 `json:"max_order_size"`
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

type GetPriceResponse struct {
	Market           string  `json:"market"`
	Price            *string `json:"price"`
	MinPrice24h      *string `json:"min_price_24h"`
	MaxPrice24h      *string `json:"max_price_24h"`
	Volume24h        *string `json:"volume_24h"`
	ChangePercent24h *string `json:"change_percent_24h"`
}

func (h *MarketHandler) GetPrices(c fiber.Ctx) error {
	prices, err := h.marketService.GetPrices(c.Context())
	if err != nil {
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	response := make([]GetPriceResponse, len(prices))
	for i, p := range prices {
		response[i] = GetPriceResponse{
			Market:           commonutils.MergeMarketRef(p.BaseSymbol, p.QuoteSymbol),
			Price:            commonutils.FormatUint64Ptr(p.Price),
			MinPrice24h:      commonutils.FormatUint64Ptr(p.MinPrice24h),
			MaxPrice24h:      commonutils.FormatUint64Ptr(p.MaxPrice24h),
			Volume24h:        commonutils.FormatUint64Ptr(p.Volume24h),
			ChangePercent24h: commonutils.PercentChangePtr(p.Price, p.OpenPrice24h),
		}
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

type DepthLevelResponse struct {
	Price    uint64 `json:"price"`
	Quantity uint64 `json:"quantity"`
}

type GetDepthResponse struct {
	Market string               `json:"market"`
	Bids   []DepthLevelResponse `json:"bids"`
	Asks   []DepthLevelResponse `json:"asks"`
}

// GetDepth is the REST counterpart of the SSE stream's snapshot frame (GET /stream/:market): a
// one-shot order-book read for polling clients that don't want to hold a persistent connection
// open. Served from the same in-memory cache, so it never reads the database.
func (h *MarketHandler) GetDepth(c fiber.Ctx) error {
	market := c.Params("market")
	priceQuantum, ok := h.marketQuanta[market]
	if !ok {
		return utils.NewErrorResponse(c, fiber.StatusNotFound, "market not found")
	}

	group, err := commonutils.ParsePriceGroup(c.Query("group"), priceQuantum)
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	depth, err := h.marketService.GetDepth(c.Context(), market, group)
	if err != nil {
		if errors.Is(err, ErrMarketNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "market not found")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(GetDepthResponse{
		Market: depth.Market,
		Bids:   depthLevelsResponse(depth.Bids),
		Asks:   depthLevelsResponse(depth.Asks),
	})
}

func depthLevelsResponse(levels []DepthLevel) []DepthLevelResponse {
	out := make([]DepthLevelResponse, len(levels))
	for i, l := range levels {
		out[i] = DepthLevelResponse{Price: l.Price, Quantity: l.Quantity}
	}
	return out
}

func NewMarketHandler(logger *logger.Logger, marketService *MarketService, marketQuanta map[string]uint64) *MarketHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if marketService == nil {
		panic("market service cannot be nil")
	}
	return &MarketHandler{
		logger:        logger,
		marketService: marketService,
		marketQuanta:  marketQuanta,
	}
}
