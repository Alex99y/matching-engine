package orders_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/api/internal/orders"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type fakeValidator struct {
	userID uuid.UUID
}

func (f fakeValidator) ValidateToken(ctx context.Context, rawToken string) (*middleware.SessionInfo, error) {
	return &middleware.SessionInfo{UserID: f.userID}, nil
}

func newTestApp(repo orders.OrderRepository, cache orders.CacheService, pub orders.OrderCommandPublisher) *fiber.App {
	log := logger.NewLogger(logger.Error)
	svc := orders.NewOrderService(log, repo, cache, pub)
	h := orders.NewOrderHandler(log, svc)
	auth := fiber.Handler(middleware.Auth(log, fakeValidator{userID: uuid.New()}))

	app := fiber.New()
	// Mirrors router.go: every order route requires auth.
	app.Post("/orders/", auth, h.CreateOrder)
	app.Get("/orders/", auth, h.GetOrders)
	app.Get("/orders/:id", auth, h.GetOrder)
	app.Delete("/orders/", auth, h.CancelOrder)
	return app
}

func jsonRequest(method, url string, body any) *http.Request {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set(fiber.HeaderContentType, "application/json")
	req.Header.Set(fiber.HeaderAuthorization, "Bearer test-token")
	return req
}

func TestGetOrderHandlerInvalidID(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("GET", "/orders/not-a-uuid", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetOrderHandlerNotFound(t *testing.T) {
	repo := &fakeOrderRepository{orderByIDWithMatchesErr: repository.ErrOrderNotFound}
	app := newTestApp(repo, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("GET", "/orders/"+uuid.New().String(), nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetOrderHandlerReturnsOrder(t *testing.T) {
	id := uuid.New()
	repo := &fakeOrderRepository{orderByIDWithMatches: &repository.OrderRow{
		ID: id, HaveInstrumentID: 10, WantInstrumentID: 20, HaveQuantity: 100, WantQuantity: 200,
	}}
	app := newTestApp(repo, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("GET", "/orders/"+id.String(), nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got orders.OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != id || got.HaveQuantity != 100 {
		t.Fatalf("body = %+v, unexpected shape", got)
	}
}

func TestGetOrdersHandlerInvalidLimit(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("GET", "/orders/?limit=0", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetOrdersHandlerInvalidDate(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("GET", "/orders/?start_date=not-a-date", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetOrdersHandlerReturnsList(t *testing.T) {
	repo := &fakeOrderRepository{ordersByUser: []repository.OrderRow{
		{ID: uuid.New(), HaveInstrumentID: 10, WantInstrumentID: 20},
	}}
	app := newTestApp(repo, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("GET", "/orders/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []orders.OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
}

func TestCreateOrderHandlerEmptyBatchRejected(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("POST", "/orders/", []orders.CreateOrderRequest{}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateOrderHandlerBatchOverMaxRejected(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	reqs := make([]orders.CreateOrderRequest, orders.MaxBatchSize+1)
	for i := range reqs {
		reqs[i] = orders.CreateOrderRequest{OrderSide: oeq.BuyOrder, OrderType: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel, Market: "BTC-USDT", Price: 1, Quantity: 1}
	}

	resp, err := app.Test(jsonRequest("POST", "/orders/", reqs))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestCreateOrderHandlerAllSucceedReturns202(t *testing.T) {
	pub := &fakePublisher{}
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), pub)

	resp, err := app.Test(jsonRequest("POST", "/orders/", []orders.CreateOrderRequest{
		{OrderSide: oeq.BuyOrder, OrderType: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel, Market: "BTC-USDT", Price: 100, Quantity: 5},
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	var got orders.BatchCreateOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].Error != nil || got.Results[0].OrderID == nil {
		t.Fatalf("results = %+v, want one success", got.Results)
	}
}

func TestCreateOrderHandlerAllFailReturns422(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("POST", "/orders/", []orders.CreateOrderRequest{
		{OrderSide: oeq.BuyOrder, OrderType: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel, Market: "NOPE-NOPE", Price: 100, Quantity: 5},
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestCreateOrderHandlerPartialFailureReturns207(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("POST", "/orders/", []orders.CreateOrderRequest{
		{OrderSide: oeq.BuyOrder, OrderType: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel, Market: "BTC-USDT", Price: 100, Quantity: 5},
		{OrderSide: oeq.BuyOrder, OrderType: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel, Market: "NOPE-NOPE", Price: 100, Quantity: 5},
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", resp.StatusCode)
	}
	var got orders.BatchCreateOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Results) != 2 || got.Results[0].Error != nil || got.Results[1].Error == nil {
		t.Fatalf("results = %+v, want [success, failure]", got.Results)
	}
}

func TestCancelOrderHandlerEmptyIDsRejected(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("DELETE", "/orders/", orders.BatchCancelOrderRequest{}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCancelOrderHandlerInvalidID(t *testing.T) {
	app := newTestApp(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("DELETE", "/orders/", orders.BatchCancelOrderRequest{OrderIDs: []string{"not-a-uuid"}}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCancelOrderHandlerReturns202WithPerOrderResults(t *testing.T) {
	knownID := uuid.New()
	unknownID := uuid.New()
	repo := &fakeOrderRepository{ordersByIDs: []repository.OrderRow{
		{ID: knownID, HaveInstrumentID: 10, WantInstrumentID: 20},
	}}
	app := newTestApp(repo, btcUsdtCache(), &fakePublisher{})

	resp, err := app.Test(jsonRequest("DELETE", "/orders/", orders.BatchCancelOrderRequest{
		OrderIDs: []string{knownID.String(), unknownID.String()},
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	var got orders.BatchCancelOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Results) != 2 || got.Results[0].Error != nil || got.Results[1].Error == nil {
		t.Fatalf("results = %+v, want [success, order not found]", got.Results)
	}
}
