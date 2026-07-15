package candles

import (
	"errors"
	"strconv"

	apiutils "github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

var validIntervals = map[int64]struct{}{
	60: {}, 300: {}, 900: {}, 3600: {}, 14400: {}, 86400: {},
}

var (
	errUnknownMarket   = errors.New("unknown market")
	errInvalidInterval = errors.New("interval must be one of: 60, 300, 900, 3600, 14400, 86400")
	errMissingFrom     = errors.New("from is required")
	errMissingTo       = errors.New("to is required")
	errInvalidRange    = errors.New("from must be before to")
	errRangeTooLarge   = errors.New("range exceeds 1000 candles")
)

type CandleHandler struct {
	logger  *logger.Logger
	service *CandleService
	markets map[string]int // market ref -> DB market id
}

type candleJSON struct {
	BucketStart int64  `json:"bucket_start"`
	Open        string `json:"open"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Close       string `json:"close"`
	Volume      string `json:"volume"`
}

type candlesResponse struct {
	Interval int64        `json:"interval"`
	Candles  []candleJSON `json:"candles"`
}

func (h *CandleHandler) GetCandles(c fiber.Ctx) error {
	market := c.Params("market")
	marketID, ok := h.markets[market]
	if !ok {
		return apiutils.NewErrorResponse(c, fiber.StatusNotFound, errUnknownMarket.Error())
	}

	intervalSec, err := parseInterval(c.Query("interval"))
	if err != nil {
		return apiutils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	from, err := utils.ParseUnixTimestamp(c.Query("from"))
	if err != nil {
		return apiutils.NewErrorResponse(c, fiber.StatusBadRequest, errMissingFrom.Error())
	}

	to, err := utils.ParseUnixTimestamp(c.Query("to"))
	if err != nil {
		return apiutils.NewErrorResponse(c, fiber.StatusBadRequest, errMissingTo.Error())
	}

	if !from.Before(to) {
		return apiutils.NewErrorResponse(c, fiber.StatusBadRequest, errInvalidRange.Error())
	}

	if to.Unix()-from.Unix() > intervalSec*1000 {
		return apiutils.NewErrorResponse(c, fiber.StatusBadRequest, errRangeTooLarge.Error())
	}

	candles, err := h.service.GetCandles(c.Context(), marketID, intervalSec, from, to)
	if err != nil {
		return apiutils.NewServerErrorResponse(c, h.logger, err)
	}

	out := make([]candleJSON, len(candles))
	for i, candle := range candles {
		out[i] = candleJSON{
			BucketStart: candle.BucketStart,
			Open:        utils.FormatUint64(candle.Open),
			High:        utils.FormatUint64(candle.High),
			Low:         utils.FormatUint64(candle.Low),
			Close:       utils.FormatUint64(candle.Close),
			Volume:      utils.FormatUint64(candle.Volume),
		}
	}

	return c.JSON(candlesResponse{Interval: intervalSec, Candles: out})
}

func parseInterval(raw string) (int64, error) {
	if raw == "" {
		return 0, errInvalidInterval
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errInvalidInterval
	}
	if _, ok := validIntervals[v]; !ok {
		return 0, errInvalidInterval
	}
	return v, nil
}

func NewCandleHandler(log *logger.Logger, service *CandleService, markets map[string]int) *CandleHandler {
	if log == nil {
		panic("logger cannot be nil")
	}
	if service == nil {
		panic("service cannot be nil")
	}
	return &CandleHandler{logger: log, service: service, markets: markets}
}
