package utils

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
)

const DateQueryFormat = "2006-01-02"

// ParseDateQuery parses the named query parameter as a YYYY-MM-DD date. Returns
// (nil, nil) when the parameter is absent, and a descriptive error — suitable for
// surfacing directly as a 400 Bad Request — when present but malformed.
func ParseDateQuery(c fiber.Ctx, param string) (*time.Time, error) {
	raw := c.Query(param)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(DateQueryFormat, raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s, expected YYYY-MM-DD", param)
	}
	return &t, nil
}
