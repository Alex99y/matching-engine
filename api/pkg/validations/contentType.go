package validations

import (
	"fmt"
	"strings"

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
		ct, _, _ := strings.Cut(c.Get("Content-Type"), ";")
		if strings.TrimSpace(ct) != string(contentType) {
			return utils.NewErrorResponse(
				c,
				fiber.StatusBadRequest,
				fmt.Sprintf("Content-Type must be %s", contentType),
			)
		}
		return c.Next()
	}
}
