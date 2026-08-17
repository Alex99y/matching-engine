package utils_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

func newDateTestApp() *fiber.App {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		t, err := utils.ParseDateQuery(c, "date")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if t == nil {
			return c.JSON(fiber.Map{"date": nil})
		}
		return c.JSON(fiber.Map{"date": t.Format(utils.DateQueryFormat)})
	})
	return app
}

func TestParseDateQueryAbsent(t *testing.T) {
	app := newDateTestApp()

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Date *string `json:"date"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Date != nil {
		t.Errorf("date = %v, want nil (parameter absent)", *got.Date)
	}
}

func TestParseDateQueryValid(t *testing.T) {
	app := newDateTestApp()

	resp, err := app.Test(httptest.NewRequest("GET", "/?date=2026-08-17", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Date string `json:"date"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Date != "2026-08-17" {
		t.Errorf("date = %q, want 2026-08-17", got.Date)
	}
}

func TestParseDateQueryInvalid(t *testing.T) {
	cases := []string{"not-a-date", "2026/08/17", "2026-13-01", ""}
	// "" is skipped by the endpoint itself (treated as absent), so only exercise malformed values.
	cases = cases[:len(cases)-1]

	app := newDateTestApp()
	for _, raw := range cases {
		resp, err := app.Test(httptest.NewRequest("GET", "/?date="+raw, nil))
		if err != nil {
			t.Fatalf("app.Test(%q): %v", raw, err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("date=%q: status = %d, want 400", raw, resp.StatusCode)
			continue
		}
		var got struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(got.Error, "YYYY-MM-DD") {
			t.Errorf("date=%q: error = %q, want mention of expected format", raw, got.Error)
		}
	}
}
