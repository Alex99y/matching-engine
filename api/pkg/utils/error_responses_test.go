package utils_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

func TestNewErrorResponse(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "bad input")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var got utils.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Message != "bad input" {
		t.Errorf("message = %q, want %q", got.Message, "bad input")
	}
}

// go-context-logging requires errors be logged once, at the origin; go-layer-architecture
// requires infrastructure errors never leak into the API response body. This exercises both:
// the raw error is logged (side effect, not asserted here) but never echoed back to the client.
func TestNewServerErrorResponseHidesInternalDetail(t *testing.T) {
	log := logger.NewLogger(logger.Error)
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return utils.NewServerErrorResponse(c, log, errors.New("dial tcp 10.0.0.5:5432: connection refused"))
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var got utils.ErrorResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(got.Message, "Internal server error") {
		t.Errorf("message = %q, want it to start with %q", got.Message, "Internal server error")
	}
	if strings.Contains(got.Message, "10.0.0.5") || strings.Contains(got.Message, "connection refused") {
		t.Errorf("response leaked internal error detail: %q", got.Message)
	}
}
