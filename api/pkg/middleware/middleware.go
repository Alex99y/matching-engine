package middleware

import (
	"fmt"
	"strconv"

	"github.com/alex99y/matching-engine/api/internal/metrics"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
)

// AccessLog logs every request and records the me_api_http_* metrics. apiMetrics may be nil,
// which disables recording (the logging path is unaffected).
func AccessLog(
	logger *logger.Logger,
	apiMetrics *metrics.ApiMetrics,
) fiber.Handler {
	return func(c fiber.Ctx) error {
		stopTimer := observability.StartTimer()
		apiMetrics.IncInFlight()
		defer apiMetrics.DecInFlight()

		err := c.Next()

		status := c.Response().StatusCode()
		lat := stopTimer()

		if err != nil {
			if fe, ok := err.(*fiber.Error); ok {
				status = fe.Code
			} else {
				status = fiber.StatusInternalServerError
			}
			_ = c.Status(status)
		}

		logger.Info(
			fmt.Sprintf("http_request [%d %s %s] %dms %s id: %s",
				status,
				c.Method(),
				c.Path(),
				lat.Milliseconds(),
				c.IP(),
				requestid.FromContext(c),
			))

		// Skip metrics for unmatched routes: their path is not a bounded template, so labeling
		// by it would blow up cardinality (already skipped for logging-as-traffic purposes).
		if status == fiber.StatusNotFound || status == fiber.StatusMethodNotAllowed {
			return err
		}

		// Label by the registered route template (e.g. /api/v1/orders/:id), never the raw path.
		route := c.Path()
		if r := c.Route(); r != nil && r.Path != "" {
			route = r.Path
		}
		apiMetrics.RecordHTTPRequest(c.Method(), route, strconv.Itoa(status), lat)

		return err
	}
}
