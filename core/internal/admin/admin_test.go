package admin_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/core/internal/admin"
	"github.com/gofiber/fiber/v3"
)

const testToken = "s3cret-token"

type fakeMarket struct{ paused bool }

func (f *fakeMarket) Pause()         { f.paused = true }
func (f *fakeMarket) Resume()        { f.paused = false }
func (f *fakeMarket) IsPaused() bool { return f.paused }

func newTestApp(t *testing.T, markets map[string]admin.MarketController) *fiber.App {
	t.Helper()
	log := logger.NewLogger(logger.Error)
	app := fiber.New()
	admin.RegisterAdminRoutes(app, testToken, admin.NewHandler(admin.NewService(markets), log))
	return app
}

// request drives the Fiber app directly rather than over a socket, so the tests need no free port.
func request(t *testing.T, app *fiber.App, method, path, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func TestPauseAndResumeReachTheMarket(t *testing.T) {
	eth := &fakeMarket{}
	app := newTestApp(t, map[string]admin.MarketController{"ETH-USDT": eth})

	status, body := request(t, app, http.MethodPost, "/admin/markets/ETH-USDT/pause", testToken)
	if status != http.StatusOK {
		t.Fatalf("pause = %d (%s), want 200", status, body)
	}
	if !eth.paused {
		t.Fatal("pause did not reach the market")
	}

	var got admin.MarketStatus
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Market != "ETH-USDT" || !got.Paused {
		t.Fatalf("pause returned %+v, want ETH-USDT paused", got)
	}

	if status, body = request(t, app, http.MethodPost, "/admin/markets/ETH-USDT/resume", testToken); status != http.StatusOK {
		t.Fatalf("resume = %d (%s), want 200", status, body)
	}
	if eth.paused {
		t.Fatal("resume did not reach the market")
	}
}

// Halting the wrong core is an easy mistake with several running; the operator must be told rather
// than getting a 200 that silently did nothing.
func TestUnservedMarketIs404(t *testing.T) {
	app := newTestApp(t, map[string]admin.MarketController{"ETH-USDT": &fakeMarket{}})

	status, _ := request(t, app, http.MethodPost, "/admin/markets/BTC-USDT/pause", testToken)
	if status != http.StatusNotFound {
		t.Fatalf("pause of an unserved market = %d, want 404", status)
	}
}

// These routes stop a market trading, so an unauthenticated caller must never reach the service.
func TestAuthIsRequired(t *testing.T) {
	eth := &fakeMarket{}
	app := newTestApp(t, map[string]admin.MarketController{"ETH-USDT": eth})

	tests := []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"wrong token", "not-the-token"},
		{"right token with extra suffix", testToken + "x"},
		{"prefix of the real token", testToken[:4]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := request(t, app, http.MethodPost, "/admin/markets/ETH-USDT/pause", tt.token)
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", status)
			}
			if eth.paused {
				t.Fatal("an unauthorized request halted the market")
			}
		})
	}
}

func TestStatusListsEveryServedMarketSorted(t *testing.T) {
	app := newTestApp(t, map[string]admin.MarketController{
		"ETH-USDT": &fakeMarket{},
		"BTC-USDT": &fakeMarket{paused: true},
		"ETH-BTC":  &fakeMarket{},
	})

	status, body := request(t, app, http.MethodGet, "/admin/markets", testToken)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, body)
	}

	var got []admin.MarketStatus
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d markets, want 3: %+v", len(got), got)
	}
	// Sorted, so an operator reading the table sees a stable order run to run.
	if got[0].Market != "BTC-USDT" || got[1].Market != "ETH-BTC" || got[2].Market != "ETH-USDT" {
		t.Fatalf("markets not sorted: %+v", got)
	}
	if !got[0].Paused || got[1].Paused || got[2].Paused {
		t.Fatalf("paused flags wrong: %+v", got)
	}
}

// An empty token would leave an anonymous trading-halt button on the network, so core must refuse
// to build the server rather than start without auth.
func TestNewServerRejectsAnEmptyToken(t *testing.T) {
	log := logger.NewLogger(logger.Error)
	handler := admin.NewHandler(admin.NewService(map[string]admin.MarketController{}), log)

	_, err := admin.NewServer(0, "", handler, log)
	if err == nil {
		t.Fatal("NewServer accepted an empty token")
	}
	if !strings.Contains(err.Error(), "ADMIN_TOKEN") {
		t.Fatalf("error %q should name the variable an operator has to set", err)
	}
}
