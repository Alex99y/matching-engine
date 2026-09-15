package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/core/internal/admin"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const testToken = "s3cret-token"

type fakeMarket struct{ paused bool }

func (f *fakeMarket) Pause()                                                  { f.paused = true }
func (f *fakeMarket) Resume()                                                 { f.paused = false }
func (f *fakeMarket) IsPaused() bool                                          { return f.paused }
func (f *fakeMarket) EmitCancel(ctx context.Context, orderID uuid.UUID) error { return nil }

func newTestApp(t *testing.T, markets map[string]admin.MarketController) *fiber.App {
	t.Helper()
	log := logger.NewLogger(logger.Error)
	app := fiber.New()
	svc := admin.NewService(markets, &fakeUsers{byName: map[string]*repository.User{}}, &fakeOrders{}, &fakeCache{})
	admin.RegisterAdminRoutes(app, testToken, admin.NewHandler(svc, log))
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
	svc := admin.NewService(map[string]admin.MarketController{},
		&fakeUsers{byName: map[string]*repository.User{}}, &fakeOrders{}, &fakeCache{})
	handler := admin.NewHandler(svc, log)

	_, err := admin.NewServer(0, "", handler, log)
	if err == nil {
		t.Fatal("NewServer accepted an empty token")
	}
	if !strings.Contains(err.Error(), "ADMIN_TOKEN") {
		t.Fatalf("error %q should name the variable an operator has to set", err)
	}
}

// --- user order management -------------------------------------------------------------------

const (
	ethMarketID = 1
	btcMarketID = 2
	// A market that exists in the database but that this core does not run a matcher for.
	unservedMarketID = 3
)

type fakeUsers struct{ byName map[string]*repository.User }

func (f *fakeUsers) GetUserByUsername(ctx context.Context, username string) (*repository.User, error) {
	user, ok := f.byName[username]
	if !ok {
		return nil, repository.ErrUserNotFound
	}
	return user, nil
}

// fakeOrders enforces the user_id filter the real queries apply, so the cross-user test asserts
// something real rather than passing because the fake ignores scoping.
type fakeOrders struct{ rows []repository.OrderRow }

func (f *fakeOrders) GetOrdersByUser(ctx context.Context, userID uuid.UUID, showOpen, showCancelled bool,
	_, _ *int, _, _ *time.Time, limit int) ([]repository.OrderRow, error) {
	var out []repository.OrderRow
	for _, r := range f.rows {
		if r.UserID != userID {
			continue
		}
		// Mirrors the repository's open-only filter: resting orders plus pending bracket exits.
		if showOpen && !showCancelled && r.MarketID == nil && r.Status != repository.OrderStatusPending {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeOrders) GetOrdersByIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]repository.OrderRow, error) {
	want := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	var out []repository.OrderRow
	for _, r := range f.rows {
		if r.UserID != userID {
			continue
		}
		if _, ok := want[r.ID]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

const (
	ethInstrumentID  = 10
	btcInstrumentID  = 11
	usdtInstrumentID = 20
)

type fakeCache struct{}

func (f *fakeCache) GetMarkets() []repository.Market {
	return []repository.Market{
		{ID: ethMarketID, BaseSymbol: "ETH", QuoteSymbol: "USDT", BaseInstrumentID: ethInstrumentID, QuoteInstrumentID: usdtInstrumentID},
		{ID: btcMarketID, BaseSymbol: "BTC", QuoteSymbol: "USDT", BaseInstrumentID: btcInstrumentID, QuoteInstrumentID: usdtInstrumentID},
		{ID: unservedMarketID, BaseSymbol: "ETH", QuoteSymbol: "BTC", BaseInstrumentID: ethInstrumentID, QuoteInstrumentID: btcInstrumentID},
	}
}

func (f *fakeCache) GetMarketByID(id int) (*repository.Market, error) {
	for _, m := range f.GetMarkets() {
		if m.ID == id {
			return &m, nil
		}
	}
	return nil, repository.ErrMarketNotFound
}

// cancellingMarket records what the admin path published, so a test can tell "queued" apart from
// "reported as queued but never sent".
type cancellingMarket struct {
	fakeMarket
	cancelled []uuid.UUID
}

func (m *cancellingMarket) EmitCancel(ctx context.Context, orderID uuid.UUID) error {
	m.cancelled = append(m.cancelled, orderID)
	return nil
}

func openOrder(id, userID uuid.UUID, marketID int) repository.OrderRow {
	m := marketID
	side := "buy"
	return repository.OrderRow{ID: id, UserID: userID, MarketID: &m, Side: &side, Type: "limit", TimeInForce: "GTC"}
}

func closedOrder(id, userID uuid.UUID) repository.OrderRow {
	return repository.OrderRow{ID: id, UserID: userID, Type: "limit", TimeInForce: "GTC"}
}

// pendingExit is a parked bracket exit: no open_orders row, so no MarketID — only its instrument
// pair says which market it belongs to. A sell exit has base as its have instrument.
func pendingExit(id, userID uuid.UUID, haveInstrumentID, wantInstrumentID int) repository.OrderRow {
	return repository.OrderRow{
		ID: id, UserID: userID, Type: "market", TimeInForce: "IOC",
		Status:           repository.OrderStatusPending,
		HaveInstrumentID: haveInstrumentID,
		WantInstrumentID: wantInstrumentID,
	}
}

type userFixture struct {
	app    *fiber.App
	eth    *cancellingMarket
	btc    *cancellingMarket
	alice  *repository.User
	orders *fakeOrders
}

func newUserFixture(t *testing.T, frozen bool, rows func(aliceID uuid.UUID) []repository.OrderRow) *userFixture {
	t.Helper()
	alice := &repository.User{ID: uuid.New(), Username: "alice", Frozen: frozen}
	eth, btc := &cancellingMarket{}, &cancellingMarket{}
	orders := &fakeOrders{rows: rows(alice.ID)}

	log := logger.NewLogger(logger.Error)
	svc := admin.NewService(
		map[string]admin.MarketController{"ETH-USDT": eth, "BTC-USDT": btc},
		&fakeUsers{byName: map[string]*repository.User{"alice": alice}},
		orders,
		&fakeCache{},
	)
	app := fiber.New()
	admin.RegisterAdminRoutes(app, testToken, admin.NewHandler(svc, log))

	return &userFixture{app: app, eth: eth, btc: btc, alice: alice, orders: orders}
}

func postJSON(t *testing.T, app *fiber.App, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

// The guard that makes this safe to operate: without a freeze, a mistyped username would wipe a
// healthy user's book, and the user could re-place everything immediately anyway.
func TestCancelRefusesUnlessTheUserIsFrozen(t *testing.T) {
	f := newUserFixture(t, false, func(id uuid.UUID) []repository.OrderRow {
		return []repository.OrderRow{openOrder(uuid.New(), id, ethMarketID)}
	})

	status, body := postJSON(t, f.app, "/admin/users/alice/orders/cancel", `{"all":true}`)
	if status != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409", status, body)
	}
	if !strings.Contains(string(body), "freeze") {
		t.Fatalf("409 body should tell the operator to freeze first, got %s", body)
	}
	if len(f.eth.cancelled) != 0 {
		t.Fatal("orders were cancelled for an unfrozen user")
	}
}

func TestCancelAllPublishesEveryOpenOrderAcrossMarkets(t *testing.T) {
	ethOrder, btcOrder, closed := uuid.New(), uuid.New(), uuid.New()
	f := newUserFixture(t, true, func(id uuid.UUID) []repository.OrderRow {
		return []repository.OrderRow{
			openOrder(ethOrder, id, ethMarketID),
			openOrder(btcOrder, id, btcMarketID),
			closedOrder(closed, id),
		}
	})

	status, body := postJSON(t, f.app, "/admin/users/alice/orders/cancel", `{"all":true}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, body)
	}

	var summary admin.CancelSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 2 || summary.Skipped != 0 {
		t.Fatalf("queued=%d skipped=%d, want 2/0 — closed orders are filtered out by the query: %+v",
			summary.Queued, summary.Skipped, summary.Results)
	}
	if len(f.eth.cancelled) != 1 || f.eth.cancelled[0] != ethOrder {
		t.Fatalf("ETH market saw %v, want [%s]", f.eth.cancelled, ethOrder)
	}
	if len(f.btc.cancelled) != 1 || f.btc.cancelled[0] != btcOrder {
		t.Fatalf("BTC market saw %v, want [%s]", f.btc.cancelled, btcOrder)
	}
}

// An order this core cannot reach must never come back as a success: it is still live, and the
// operator has to know that to act on it.
func TestCancelReportsUnreachableOrdersWithoutBlockingTheRest(t *testing.T) {
	reachable, onPaused, onUnserved, closed := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f := newUserFixture(t, true, func(id uuid.UUID) []repository.OrderRow {
		return []repository.OrderRow{
			openOrder(reachable, id, ethMarketID),
			openOrder(onPaused, id, btcMarketID),
			openOrder(onUnserved, id, unservedMarketID),
			closedOrder(closed, id),
		}
	})
	f.btc.Pause()

	// Explicit ids, so the closed order is included rather than filtered out by the open-only query.
	body := fmt.Sprintf(`{"order_ids":[%q,%q,%q,%q]}`, reachable, onPaused, onUnserved, closed)
	status, raw := postJSON(t, f.app, "/admin/users/alice/orders/cancel", body)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200 — unreachable orders are results, not request failures", status, raw)
	}

	var summary admin.CancelSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 1 || summary.Skipped != 3 {
		t.Fatalf("queued=%d skipped=%d, want 1/3: %+v", summary.Queued, summary.Skipped, summary.Results)
	}

	reasons := map[string]string{}
	for _, r := range summary.Results {
		reasons[r.OrderID] = r.Reason
	}
	if reasons[reachable.String()] != "" {
		t.Fatalf("reachable order was skipped: %q", reasons[reachable.String()])
	}
	for _, tc := range []struct{ id, want string }{
		{onPaused.String(), "paused"},
		{onUnserved.String(), "not served"},
		{closed.String(), "not open"},
	} {
		if !strings.Contains(reasons[tc.id], tc.want) {
			t.Fatalf("reason for %s = %q, want it to mention %q", tc.id, reasons[tc.id], tc.want)
		}
	}
	if len(f.eth.cancelled) != 1 {
		t.Fatalf("the reachable order should still have been published, got %v", f.eth.cancelled)
	}
	if len(f.btc.cancelled) != 0 {
		t.Fatal("published to a paused market")
	}
}

// A pending bracket exit holds blocked funds without an open_orders row, so the operator sweep must
// still reach it — through its instrument pair — or a frozen account could keep a live exit.
func TestCancelReachesPendingExitsThroughTheirInstrumentPair(t *testing.T) {
	onEth, onBtc, onUnserved := uuid.New(), uuid.New(), uuid.New()
	f := newUserFixture(t, true, func(id uuid.UUID) []repository.OrderRow {
		return []repository.OrderRow{
			pendingExit(onEth, id, ethInstrumentID, usdtInstrumentID), // sell exit on ETH-USDT
			pendingExit(onBtc, id, usdtInstrumentID, btcInstrumentID), // buy exit on BTC-USDT
			pendingExit(onUnserved, id, ethInstrumentID, btcInstrumentID),
		}
	})

	status, raw := postJSON(t, f.app, "/admin/users/alice/orders/cancel", `{"all":true}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, raw)
	}

	var summary admin.CancelSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 2 || summary.Skipped != 1 {
		t.Fatalf("queued=%d skipped=%d, want 2/1: %+v", summary.Queued, summary.Skipped, summary.Results)
	}
	if len(f.eth.cancelled) != 1 || f.eth.cancelled[0] != onEth {
		t.Fatalf("ETH market saw %v, want [%s]", f.eth.cancelled, onEth)
	}
	if len(f.btc.cancelled) != 1 || f.btc.cancelled[0] != onBtc {
		t.Fatalf("BTC market saw %v, want [%s]", f.btc.cancelled, onBtc)
	}
	for _, r := range summary.Results {
		if r.OrderID == onUnserved.String() && !strings.Contains(r.Reason, "not served") {
			t.Fatalf("unserved exit reason = %q, want it to mention \"not served\"", r.Reason)
		}
	}
}

// The repository queries are user-scoped; an admin acting on alice must not be able to cancel bob's
// order by pasting its id.
func TestCancelCannotReachAnotherUsersOrder(t *testing.T) {
	bobOrder := uuid.New()
	bobID := uuid.New()
	f := newUserFixture(t, true, func(aliceID uuid.UUID) []repository.OrderRow {
		return []repository.OrderRow{openOrder(bobOrder, bobID, ethMarketID)}
	})

	status, raw := postJSON(t, f.app, "/admin/users/alice/orders/cancel", fmt.Sprintf(`{"order_ids":[%q]}`, bobOrder))
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, raw)
	}

	var summary admin.CancelSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 0 || len(summary.Results) != 0 {
		t.Fatalf("another user's order was targeted: %+v", summary)
	}
	if len(f.eth.cancelled) != 0 {
		t.Fatalf("published a cancel for another user's order: %v", f.eth.cancelled)
	}
}

func TestCancelRequiresATarget(t *testing.T) {
	f := newUserFixture(t, true, func(uuid.UUID) []repository.OrderRow { return nil })

	status, _ := postJSON(t, f.app, "/admin/users/alice/orders/cancel", `{}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for neither all nor order_ids", status)
	}
}

func TestListUserOrdersReportsFrozenAndReachability(t *testing.T) {
	onEth, onPaused := uuid.New(), uuid.New()
	f := newUserFixture(t, true, func(id uuid.UUID) []repository.OrderRow {
		return []repository.OrderRow{openOrder(onEth, id, ethMarketID), openOrder(onPaused, id, btcMarketID)}
	})
	f.btc.Pause()

	status, body := request(t, f.app, http.MethodGet, "/admin/users/alice/orders", testToken)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, body)
	}

	var out admin.UserOrders
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Frozen {
		t.Fatal("frozen flag not reported")
	}
	if len(out.Orders) != 2 {
		t.Fatalf("got %d orders, want 2", len(out.Orders))
	}

	byID := map[string]admin.UserOrder{}
	for _, o := range out.Orders {
		byID[o.OrderID] = o
	}
	if !byID[onEth.String()].Reachable {
		t.Fatal("order on a running market should be reachable")
	}
	// Reachability is shown up front so the operator sees the problem before attempting the cancel.
	if paused := byID[onPaused.String()]; paused.Reachable || !strings.Contains(paused.Unreachable, "paused") {
		t.Fatalf("order on a paused market = %+v, want unreachable with a paused reason", paused)
	}
}

func TestUnknownUserIs404(t *testing.T) {
	f := newUserFixture(t, true, func(uuid.UUID) []repository.OrderRow { return nil })

	if status, _ := request(t, f.app, http.MethodGet, "/admin/users/nobody/orders", testToken); status != http.StatusNotFound {
		t.Fatalf("list = %d, want 404", status)
	}
	if status, _ := postJSON(t, f.app, "/admin/users/nobody/orders/cancel", `{"all":true}`); status != http.StatusNotFound {
		t.Fatalf("cancel = %d, want 404", status)
	}
}
