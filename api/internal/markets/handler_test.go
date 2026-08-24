package markets_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/api/internal/markets"
	"github.com/alex99y/matching-engine/api/internal/stream"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

func newTestApp(depthSource markets.DepthSource, marketQuanta map[string]uint64) *fiber.App {
	svc := markets.NewMarketService(logger.NewLogger(logger.Error), stubMarketRepository{}, depthSource)
	h := markets.NewMarketHandler(logger.NewLogger(logger.Error), svc, marketQuanta)

	app := fiber.New()
	app.Get("/markets/:market/depth", h.GetDepth)
	return app
}

func TestGetDepthHandlerUnknownMarket(t *testing.T) {
	app := newTestApp(fakeDepthSource{}, map[string]uint64{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/ETH-USDT/depth", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetDepthHandlerInvalidGroup(t *testing.T) {
	app := newTestApp(
		fakeDepthSource{found: true, depth: &stream.MarketDepth{Market: "BTC-USDT"}},
		map[string]uint64{"BTC-USDT": 5},
	)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/depth?group=3", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetDepthHandlerReturnsSnapshot(t *testing.T) {
	app := newTestApp(fakeDepthSource{
		found: true,
		depth: &stream.MarketDepth{
			Market: "BTC-USDT",
			Bids:   []stream.DepthLevel{{Price: 100, Quantity: 2}},
			Asks:   []stream.DepthLevel{{Price: 101, Quantity: 4}},
		},
	}, map[string]uint64{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/depth", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got markets.GetDepthResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Market != "BTC-USDT" || len(got.Bids) != 1 || got.Bids[0].Price != 100 || got.Bids[0].Quantity != 2 {
		t.Fatalf("body = %+v, want bids [{100 2}]", got)
	}
	if len(got.Asks) != 1 || got.Asks[0].Price != 101 {
		t.Fatalf("body = %+v, want asks [{101 *}]", got)
	}
}

// Defensive path: marketQuanta says the market is served, but the depth source disagrees (should
// not happen given both are built from the same startup market list — see the comment on
// MarketService.GetDepth). The handler must still map it to 404, not 500.
func TestGetDepthHandlerServiceNotFoundMapsTo404(t *testing.T) {
	app := newTestApp(fakeDepthSource{found: false}, map[string]uint64{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/depth", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetDepthHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	app := newTestApp(
		fakeDepthSource{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")},
		map[string]uint64{"BTC-USDT": 1},
	)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/depth", nil))
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
