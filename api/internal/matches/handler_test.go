package matches_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/matches"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

func newTestApp(repo matches.MatchRepository, marketIDs map[string]int) *fiber.App {
	svc := matches.NewMatchService(logger.NewLogger(logger.Error), repo)
	h := matches.NewMatchHandler(logger.NewLogger(logger.Error), svc, marketIDs)

	app := fiber.New()
	app.Get("/markets/:market/matches", h.GetMatches)
	return app
}

func TestGetMatchesHandlerUnknownMarket(t *testing.T) {
	app := newTestApp(&fakeMatchRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/ETH-USDT/matches", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetMatchesHandlerInvalidLimit(t *testing.T) {
	app := newTestApp(&fakeMatchRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/matches?limit=0", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetMatchesHandlerLimitOver100(t *testing.T) {
	app := newTestApp(&fakeMatchRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/matches?limit=101", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetMatchesHandlerInvalidDate(t *testing.T) {
	app := newTestApp(&fakeMatchRepository{}, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/matches?start_date=not-a-date", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetMatchesHandlerReturnsTradeTapeShape(t *testing.T) {
	id := uuid.New()
	matchTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeMatchRepository{rows: []repository.Match{
		{ID: id, Price: 2010000000, Quantity: 500000, TakerSide: "buy", MatchTime: matchTime},
	}}
	app := newTestApp(repo, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/matches", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got []matches.MatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	m := got[0]
	if m.ID != id.String() || m.Price != 2010000000 || m.Quantity != 500000 ||
		m.TakerSide != "buy" || m.MatchTime != matchTime.Unix() {
		t.Fatalf("match = %+v, unexpected shape", m)
	}

	// Deliberately not present on the wire: buy_order_id / sell_order_id (see api/internal/matches
	// design decision — this is a public endpoint, other users' order ids must never leak here).
	var raw []map[string]any
	resp2, _ := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/matches", nil))
	body, _ := io.ReadAll(resp2.Body)
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw[0]["buy_order_id"]; ok {
		t.Error("response leaked buy_order_id")
	}
	if _, ok := raw[0]["sell_order_id"]; ok {
		t.Error("response leaked sell_order_id")
	}
}

func TestGetMatchesHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &fakeMatchRepository{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo, map[string]int{"BTC-USDT": 1})

	resp, err := app.Test(httptest.NewRequest("GET", "/markets/BTC-USDT/matches", nil))
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
