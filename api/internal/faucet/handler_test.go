package faucet_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gofiber/fiber/v3"

	"github.com/alex99y/matching-engine/api/internal/faucet"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type fakeValidator struct {
	userID uuid.UUID
}

func (f fakeValidator) ValidateToken(ctx context.Context, rawToken string) (*middleware.SessionInfo, error) {
	return &middleware.SessionInfo{UserID: f.userID}, nil
}

func newTestApp(instrRepo faucet.InstrumentRepository, userRepo faucet.UserRepository) *fiber.App {
	log := logger.NewLogger(logger.Error)
	svc := faucet.NewFaucetService(log, instrRepo, userRepo)
	h := faucet.NewFaucetHandler(log, svc)

	app := fiber.New()
	app.Use(fiber.Handler(middleware.Auth(log, fakeValidator{userID: uuid.New()})))
	app.Post("/faucet", h.RequestFunds)
	return app
}

func newFaucetRequest(query string) *http.Request {
	req := httptest.NewRequest("POST", "/faucet"+query, nil)
	req.Header.Set(fiber.HeaderAuthorization, "Bearer test-token")
	return req
}

func TestRequestFundsHandlerMissingInstrument(t *testing.T) {
	app := newTestApp(fakeInstrumentRepository{}, &fakeUserRepository{})

	resp, err := app.Test(newFaucetRequest(""))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRequestFundsHandlerInstrumentNotFound(t *testing.T) {
	app := newTestApp(fakeInstrumentRepository{err: repository.ErrInstrumentNotFound}, &fakeUserRepository{})

	resp, err := app.Test(newFaucetRequest("?instrument=NOPE"))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRequestFundsHandlerReturnsCredit(t *testing.T) {
	instrRepo := fakeInstrumentRepository{instr: &repository.Instrument{ID: 1, Symbol: "BTC", Decimals: 8}}
	app := newTestApp(instrRepo, &fakeUserRepository{})

	resp, err := app.Test(newFaucetRequest("?instrument=BTC"))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got faucet.FaucetResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Symbol != "BTC" || got.Amount != 10_000_000 {
		t.Fatalf("body = %+v, want {BTC 10000000}", got)
	}
}

func TestRequestFundsHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	instrRepo := fakeInstrumentRepository{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(instrRepo, &fakeUserRepository{})

	resp, err := app.Test(newFaucetRequest("?instrument=BTC"))
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
	if strings.Contains(string(body), "10.0.0.5") {
		t.Errorf("response leaked internal error detail: %s", body)
	}
}

func TestRequestFundsHandlerRequiresAuth(t *testing.T) {
	app := newTestApp(fakeInstrumentRepository{}, &fakeUserRepository{})

	req := httptest.NewRequest("POST", "/faucet?instrument=BTC", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
