package matches

import (
	"errors"
	"strconv"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

var errUnknownMarket = errors.New("unknown market")

type MatchHandler struct {
	logger  *logger.Logger
	service *MatchService
	markets map[string]int // market ref -> DB market id
}

type MatchResponse struct {
	ID        string `json:"id"`
	Price     uint64 `json:"price"`
	Quantity  uint64 `json:"quantity"`
	TakerSide string `json:"taker_side"`
	MatchTime int64  `json:"match_time"`
}

func (h *MatchHandler) GetMatches(c fiber.Ctx) error {
	market := c.Params("market")
	marketID, ok := h.markets[market]
	if !ok {
		return utils.NewErrorResponse(c, fiber.StatusNotFound, errUnknownMarket.Error())
	}

	var filter GetMatchesFilter

	startDate, err := utils.ParseDateQuery(c, "start_date")
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	filter.StartDate = startDate

	endDate, err := utils.ParseDateQuery(c, "end_date")
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	filter.EndDate = endDate

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid limit, must be a positive integer")
		}
		filter.Limit = n
	}

	matchesResult, err := h.service.GetMatches(c.Context(), marketID, filter)
	if err != nil {
		if errors.Is(err, ErrInvalidLimit) {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "limit must be between 1 and 100")
		}
		return utils.NewServerErrorResponse(c, h.logger, err)
	}

	out := make([]MatchResponse, len(matchesResult))
	for i, m := range matchesResult {
		out[i] = MatchResponse{
			ID:        m.ID.String(),
			Price:     m.Price,
			Quantity:  m.Quantity,
			TakerSide: m.TakerSide,
			MatchTime: m.MatchTime.Unix(),
		}
	}
	return c.JSON(out)
}

func NewMatchHandler(log *logger.Logger, service *MatchService, markets map[string]int) *MatchHandler {
	if log == nil {
		panic("logger cannot be nil")
	}
	if service == nil {
		panic("service cannot be nil")
	}
	return &MatchHandler{logger: log, service: service, markets: markets}
}
