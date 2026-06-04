package utils

import (
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
)

type ErrorResponse struct {
	Message string `json:"message"`
}

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
	return c.Status(status).JSON(ErrorResponse{Message: message})
}
