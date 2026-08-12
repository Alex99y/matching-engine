package openapi

import (
	_ "embed"

	"github.com/gofiber/fiber/v3"
)

//go:embed swagger.yaml
var spec []byte

func GetSpec(c fiber.Ctx) error {
	// text/* (not application/yaml) so browsers render it inline instead of downloading it —
	// browsers have a built-in viewer for text/*, but not for the unregistered application/yaml.
	c.Set(fiber.HeaderContentType, "text/yaml; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, "inline")
	return c.Send(spec)
}
