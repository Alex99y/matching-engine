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
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/gofiber/fiber/v3"
)

func newTestApp(depthSource markets.DepthSource, marketQuanta map[string]uint64) *fiber.App {
	return newTestAppWithRepo(stubMarketRepository{}, depthSource, marketQuanta)
}

func newTestAppWithRepo(repo markets.MarketRepository, depthSource markets.DepthSource, marketQuanta map[string]uint64) *fiber.App {
	svc := markets.NewMarketService(logger.NewLogger(logger.Error), repo, depthSource)
	h := markets.NewMarketHandler(logger.NewLogger(logger.Error), svc, marketQuanta)

	app := fiber.New()
	// Mirrors router.go's actual route order (/:market must come after the literal paths).
	app.Get("/markets/", h.GetMarkets)
	app.Get("/markets/prices", h.GetPrices)
	app.Get("/markets/:market", h.GetMarket)
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

func TestGetPricesHandlerIncludesChangePercentButNotRawOpenPrice(t *testing.T) {
	u64 := func(v uint64) *uint64 { return &v }
	repo := stubMarketRepository{prices: []repository.MarketPrice{
		{
			BaseSymbol: "BTC", QuoteSymbol: "USDT",
			Price: u64(110), MinPrice24h: u64(90), MaxPrice24h: u64(120),
			Volume24h: u64(5), OpenPrice24h: u64(100),
		},
	}}
	app := newTestAppWithRepo(repo, fakeDepthSource{}, nil)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/prices", nil))
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
	// The raw open price is used server-side to compute the percentage but must never reach
	// the wire — check the actual JSON, not just the (compile-time-safe) Go struct shape.
	if strings.Contains(string(body), "open_price") {
		t.Fatalf("response leaked the raw open price: %s", body)
	}

	var got []markets.GetPriceResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].ChangePercent24h == nil || *got[0].ChangePercent24h != "10.00" {
		t.Fatalf("ChangePercent24h = %v, want \"10.00\"", got[0].ChangePercent24h)
	}
}

func TestGetMarketHandlerNotFound(t *testing.T) {
	app := newTestAppWithRepo(&spyMarketRepository{marketErr: repository.ErrMarketNotFound}, fakeDepthSource{}, nil)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetMarketHandlerReturnsMarket(t *testing.T) {
	repo := &spyMarketRepository{market: &repository.Market{
		BaseSymbol: "BTC", QuoteSymbol: "USDT", PriceQuantum: 1, AmountQuantum: 1000, MinOrderSize: 1000, MaxOrderSize: 1000000000,
		TakerFeeBps: 100, MakerFeeBps: 50,
	}}
	app := newTestAppWithRepo(repo, fakeDepthSource{}, nil)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got markets.GetMarketResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := markets.GetMarketResponse{
		BaseSymbol: "BTC", QuoteSymbol: "USDT", PriceQuantum: 1, AmountQuantum: 1000,
		MinOrderSize: 1000, MaxOrderSize: 1000000000, TakerFeeBps: 100, MakerFeeBps: 50,
	}
	if got != want {
		t.Fatalf("body = %+v, want %+v", got, want)
	}
}

func TestGetMarketHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &spyMarketRepository{marketErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestAppWithRepo(repo, fakeDepthSource{}, nil)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT", nil))
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

func TestGetMarketsHandlerReturnsList(t *testing.T) {
	repo := &spyMarketRepository{markets: []repository.Market{
		{BaseSymbol: "BTC", QuoteSymbol: "USDT"},
		{BaseSymbol: "ETH", QuoteSymbol: "USDT"},
	}}
	app := newTestAppWithRepo(repo, fakeDepthSource{}, nil)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []markets.GetMarketResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].BaseSymbol != "BTC" || got[1].BaseSymbol != "ETH" {
		t.Fatalf("body = %+v, want [BTC ETH]", got)
	}
}

func TestGetMarketsHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &spyMarketRepository{marketsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestAppWithRepo(repo, fakeDepthSource{}, nil)

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/", nil))
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
