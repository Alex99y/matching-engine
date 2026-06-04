package validations

import (
	"fmt"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type ContentType string

const (
	ContentTypeJSON ContentType = "application/json"
)

type ValidateContentTypeError struct {
	Message string
}

func ValidateContentType(contentType ContentType) fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Get("Content-Type") != string(contentType) {
			return utils.NewErrorResponse(
				c,
				fiber.StatusBadRequest,
				fmt.Sprintf("Content-Type must be %s", contentType),
			)
		}
		return c.Next()
	}
}
