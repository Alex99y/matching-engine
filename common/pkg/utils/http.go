package utils

import (
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// ErrorResponse is the body every service returns for a failed request. Shared so a client can rely
// on one shape regardless of which service answered.
type ErrorResponse struct {
	Message string `json:"message"`
}

func NewErrorResponse(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(ErrorResponse{Message: message})
}

// APIErrorMessage extracts the message from an ErrorResponse body, falling back to the raw body so a
// reply from something that is not one of our services — a proxy, or the wrong port — is still
// legible to whoever is reading the error.
func APIErrorMessage(body []byte) string {
	var payload ErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	return strings.TrimSpace(string(body))
}
