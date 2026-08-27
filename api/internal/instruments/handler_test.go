package instruments_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/instruments"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/gofiber/fiber/v3"
)

func newTestApp(repo instruments.InstrumentRepository) *fiber.App {
	svc := instruments.NewInstrumentService(logger.NewLogger(logger.Error), repo)
	h := instruments.NewInstrumentHandler(logger.NewLogger(logger.Error), svc)

	app := fiber.New()
	app.Get("/instruments/", h.GetInstruments)
	app.Get("/instruments/:symbol", h.GetInstrument)
	return app
}

func TestGetInstrumentHandlerNotFound(t *testing.T) {
	app := newTestApp(&fakeInstrumentRepository{instrErr: repository.ErrInstrumentNotFound})

	resp, err := app.Test(httptest.NewRequest("GET", "/instruments/NOPE", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetInstrumentHandlerReturnsInstrument(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeInstrumentRepository{instr: &repository.Instrument{
		Name: "Bitcoin", Symbol: "BTC", Decimals: 8, CreatedAt: created,
	}}
	app := newTestApp(repo)

	resp, err := app.Test(httptest.NewRequest("GET", "/instruments/BTC", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got instruments.GetInstrumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Bitcoin" || got.Symbol != "BTC" || got.Decimals != 8 {
		t.Fatalf("body = %+v, unexpected shape", got)
	}
}

func TestGetInstrumentHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &fakeInstrumentRepository{instrErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo)

	resp, err := app.Test(httptest.NewRequest("GET", "/instruments/BTC", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "10.0.0.5") {
		t.Errorf("response leaked internal error detail: %s", body)
	}
}

func TestGetInstrumentsHandlerReturnsList(t *testing.T) {
	repo := &fakeInstrumentRepository{instrs: []repository.Instrument{
		{Name: "Bitcoin", Symbol: "BTC", Decimals: 8},
		{Name: "Tether", Symbol: "USDT", Decimals: 6},
	}}
	app := newTestApp(repo)

	resp, err := app.Test(httptest.NewRequest("GET", "/instruments/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got []instruments.GetInstrumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "BTC" || got[1].Symbol != "USDT" {
		t.Fatalf("body = %+v, want [BTC USDT]", got)
	}
}

func TestGetInstrumentsHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &fakeInstrumentRepository{instrsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo)

	resp, err := app.Test(httptest.NewRequest("GET", "/instruments/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "10.0.0.5") {
		t.Errorf("response leaked internal error detail: %s", body)
	}
}
