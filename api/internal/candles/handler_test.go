package candles_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/api/internal/candles"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/gofiber/fiber/v3"
)

func newTestApp(repo candles.CandleRepository, markets map[string]int) *fiber.App {
	svc := candles.NewCandleService(logger.NewLogger(logger.Error), repo)
	h := candles.NewCandleHandler(logger.NewLogger(logger.Error), svc, markets)

	app := fiber.New()
	app.Get("/markets/:market/candles", h.GetCandles)
	return app
}

func TestGetCandlesHandlerUnknownMarket(t *testing.T) {
	app := newTestApp(&fakeCandleRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/ETH-USDT/candles?interval=60&from=0&to=60", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetCandlesHandlerInvalidInterval(t *testing.T) {
	app := newTestApp(&fakeCandleRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/candles?interval=42&from=0&to=60", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetCandlesHandlerMissingFrom(t *testing.T) {
	app := newTestApp(&fakeCandleRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/candles?interval=60&to=60", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetCandlesHandlerFromNotBeforeTo(t *testing.T) {
	app := newTestApp(&fakeCandleRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/candles?interval=60&from=100&to=100", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetCandlesHandlerRangeTooLarge(t *testing.T) {
	app := newTestApp(&fakeCandleRepository{}, map[string]int{"BTC-USDT": 1})

	// interval=60 allows at most 60*1000 = 60000 seconds; ask for one more.
	url := "/markets/BTC-USDT/candles?interval=60&from=0&to=" + strconv.Itoa(60001)
	resp, err := app.Test(httptest.NewRequest("GET", url, nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetCandlesHandlerReturnsStringEncodedAmounts(t *testing.T) {
	repo := &fakeCandleRepository{rows: []repository.Candle{
		{BucketStart: 1000, Open: 100, High: 120, Low: 90, Close: 110, Volume: 5},
	}}
	app := newTestApp(repo, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/candles?interval=60&from=0&to=60", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var got struct {
		Interval int64 `json:"interval"`
		Candles  []struct {
			BucketStart int64  `json:"bucket_start"`
			Open        string `json:"open"`
			High        string `json:"high"`
			Low         string `json:"low"`
			Close       string `json:"close"`
			Volume      string `json:"volume"`
		} `json:"candles"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Interval != 60 || len(got.Candles) != 1 {
		t.Fatalf("body = %+v, unexpected shape", got)
	}
	c := got.Candles[0]
	// The wire amounts are strings, not numbers, since uint64 loses precision through
	// JavaScript's float64 JSON numbers — assert against the marshaled string, not just the
	// (compile-time-safe) Go struct shape.
	if c.Open != "100" || c.High != "120" || c.Low != "90" || c.Close != "110" || c.Volume != "5" {
		t.Fatalf("candle = %+v, want string-encoded amounts", c)
	}
}

func TestGetCandlesHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &fakeCandleRepository{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/candles?interval=60&from=0&to=60", nil))
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
