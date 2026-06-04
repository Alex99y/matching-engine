package middleware

import (
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
)

func AccessLog(
	logger *logger.Logger,
) fiber.Handler {
	return func(c fiber.Ctx) error {
		stopTimer := observability.StartTimer()
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

		// Avoid metrics for invalid requests
		if status == fiber.StatusNotFound || status == fiber.StatusMethodNotAllowed {
			return err
		}

		return err
	}
}
