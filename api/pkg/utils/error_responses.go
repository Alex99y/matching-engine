package utils

import (
	"github.com/alex99y/matching-engine/common/pkg/logger"
	commonutils "github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
)

// ErrorResponse is an alias for the shared body so existing api references keep working while there
// is only one definition of the shape.
type ErrorResponse = commonutils.ErrorResponse

func NewServerErrorResponse(
	c fiber.Ctx,
	logger *logger.Logger,
	err error,
) error {
	logger.Error(
		"Internal server error request id: " +
			requestid.FromContext(c) + " - " +
			err.Error(),
	)
	return NewErrorResponse(
		c, fiber.StatusInternalServerError,
		"Internal server error "+requestid.FromContext(c),
	)
}

func NewErrorResponse(c fiber.Ctx, status int, message string) error {
	return commonutils.NewErrorResponse(c, status, message)
}
