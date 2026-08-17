package validations_test

import (
	"net/http/httptest"
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/gofiber/fiber/v3"
)

func newContentTypeTestApp() *fiber.App {
	app := fiber.New()
	app.Post("/", validations.ValidateContentType(validations.ContentTypeJSON), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func TestValidateContentTypeAccepts(t *testing.T) {
	cases := []string{
		"application/json",
		"application/json; charset=utf-8",
		" application/json ; charset=utf-8",
	}
	app := newContentTypeTestApp()
	for _, ct := range cases {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set(fiber.HeaderContentType, ct)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test(%q): %v", ct, err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("Content-Type %q: status = %d, want 200", ct, resp.StatusCode)
		}
	}
}

func TestValidateContentTypeRejects(t *testing.T) {
	cases := []string{"", "text/plain", "application/xml", "application/json-patch+json"}
	app := newContentTypeTestApp()
	for _, ct := range cases {
		req := httptest.NewRequest("POST", "/", nil)
		if ct != "" {
			req.Header.Set(fiber.HeaderContentType, ct)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test(%q): %v", ct, err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Content-Type %q: status = %d, want 400", ct, resp.StatusCode)
		}
	}
}
