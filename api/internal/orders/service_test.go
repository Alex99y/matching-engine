package orders_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/api/internal/orders"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var errNotFoundInFake = errors.New("not found in fake")

type fakeCacheService struct {
	marketsByRef map[string]*repository.Market
	marketErr    error

	instrumentsByID map[int]*repository.Instrument
	instrumentErr   error
}

func (f *fakeCacheService) GetMarketByRef(marketRef string) (*repository.Market, error) {
	if f.marketErr != nil {
		return nil, f.marketErr
	}
	if m, ok := f.marketsByRef[marketRef]; ok {
		return m, nil
	}
	return nil, errNotFoundInFake
}

func (f *fakeCacheService) GetInstrumentByID(id int) (*repository.Instrument, error) {
	if f.instrumentErr != nil {
		return nil, f.instrumentErr
	}
	if instr, ok := f.instrumentsByID[id]; ok {
		return instr, nil
	}
	return nil, errNotFoundInFake
}

type getOrdersByUserCall struct {
	showOpen, showCancelled bool
	baseID, quoteID         *int
	startDate, endDate      *time.Time
	limit                   int
}

type fakeOrderRepository struct {
	orderByID    *repository.OrderRow
	orderByIDErr error

	orderByIDWithMatches    *repository.OrderRow
	orderByIDWithMatchesErr error

	orderByClientID    *repository.OrderRow
	orderByClientIDErr error

	ordersByUser    []repository.OrderRow
	ordersByUserErr error
	gotUserCall     getOrdersByUserCall

	ordersByIDs    []repository.OrderRow
	ordersByIDsErr error
}

func (f *fakeOrderRepository) GetOrderByID(ctx context.Context, userID, id uuid.UUID) (*repository.OrderRow, error) {
	return f.orderByID, f.orderByIDErr
}

func (f *fakeOrderRepository) GetOrderByIDWithMatches(ctx context.Context, userID, id uuid.UUID) (*repository.OrderRow, error) {
	return f.orderByIDWithMatches, f.orderByIDWithMatchesErr
}

func (f *fakeOrderRepository) GetOrderByClientOrderID(ctx context.Context, userID uuid.UUID, clientOrderID string) (*repository.OrderRow, error) {
	return f.orderByClientID, f.orderByClientIDErr
}

func (f *fakeOrderRepository) GetOrdersByUser(ctx context.Context, userID uuid.UUID, showOpenOrders, showCancelledOrders bool, baseInstrumentID, quoteInstrumentID *int, startDate, endDate *time.Time, limit int) ([]repository.OrderRow, error) {
	f.gotUserCall = getOrdersByUserCall{showOpenOrders, showCancelledOrders, baseInstrumentID, quoteInstrumentID, startDate, endDate, limit}
	return f.ordersByUser, f.ordersByUserErr
}

func (f *fakeOrderRepository) GetOrdersByIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]repository.OrderRow, error) {
	return f.ordersByIDs, f.ordersByIDsErr
}

type publishCall struct {
	messageID string
	marketRef string
	event     *oeq.OrderEvent
}

type fakePublisher struct {
	err   error
	calls []publishCall
}

func (f *fakePublisher) Publish(ctx context.Context, messageId string, marketRef string, event *oeq.OrderEvent) error {
	f.calls = append(f.calls, publishCall{messageId, marketRef, event})
	return f.err
}

func newTestService(repo orders.OrderRepository, cache orders.CacheService, pub orders.OrderCommandPublisher) *orders.OrderService {
	return orders.NewOrderService(logger.NewLogger(logger.Error), repo, cache, pub)
}

func btcUsdtCache() *fakeCacheService {
	return &fakeCacheService{
		marketsByRef: map[string]*repository.Market{
			"BTC-USDT": {ID: 1, BaseInstrumentID: 10, QuoteInstrumentID: 20, PriceQuantum: 1, AmountQuantum: 1, MinOrderSize: 1, MaxOrderSize: 1000000000, BaseScale: 100000000},
		},
		instrumentsByID: map[int]*repository.Instrument{
			10: {ID: 10, Symbol: "BTC"},
			20: {ID: 20, Symbol: "USDT"},
		},
	}
}

func TestGetOrderByIDSuccessBackfillsSideForRestingOrder(t *testing.T) {
	repo := &fakeOrderRepository{orderByIDWithMatches: &repository.OrderRow{
		HaveInstrumentID: 10, WantInstrumentID: 20, // have=base -> sell
	}}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	got, err := svc.GetOrderByID(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("GetOrderByID: %v", err)
	}
	if got.Side == nil || *got.Side != "sell" {
		t.Fatalf("Side = %v, want sell", got.Side)
	}
}

func TestGetOrderByIDNotFound(t *testing.T) {
	repo := &fakeOrderRepository{orderByIDWithMatchesErr: repository.ErrOrderNotFound}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	_, err := svc.GetOrderByID(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, orders.ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

func TestGetOrdersByClientOrderID(t *testing.T) {
	repo := &fakeOrderRepository{orderByClientID: &repository.OrderRow{
		ClientOrderID: "abc", HaveInstrumentID: 20, WantInstrumentID: 10, // have=quote -> buy
	}}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	got, err := svc.GetOrders(context.Background(), uuid.New(), orders.GetOrdersFilter{ClientOrderID: "abc"})
	if err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	if len(got) != 1 || got[0].ClientOrderID != "abc" || got[0].Side == nil || *got[0].Side != "buy" {
		t.Fatalf("got = %+v, want one buy order", got)
	}
}

func TestGetOrdersByClientOrderIDNotFound(t *testing.T) {
	repo := &fakeOrderRepository{orderByClientIDErr: repository.ErrOrderNotFound}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	_, err := svc.GetOrders(context.Background(), uuid.New(), orders.GetOrdersFilter{ClientOrderID: "abc"})
	if !errors.Is(err, orders.ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

func TestGetOrdersDefaultsLimitTo10(t *testing.T) {
	repo := &fakeOrderRepository{}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	if _, err := svc.GetOrders(context.Background(), uuid.New(), orders.GetOrdersFilter{}); err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	if repo.gotUserCall.limit != 10 {
		t.Fatalf("limit passed to repo = %d, want 10 (default)", repo.gotUserCall.limit)
	}
}

func TestGetOrdersRejectsLimitOver100(t *testing.T) {
	svc := newTestService(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	_, err := svc.GetOrders(context.Background(), uuid.New(), orders.GetOrdersFilter{Limit: 101})
	if !errors.Is(err, orders.ErrInvalidLimit) {
		t.Fatalf("err = %v, want ErrInvalidLimit", err)
	}
}

func TestGetOrdersFiltersByMarketResolvesInstrumentIDs(t *testing.T) {
	repo := &fakeOrderRepository{}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	if _, err := svc.GetOrders(context.Background(), uuid.New(), orders.GetOrdersFilter{Market: "BTC-USDT"}); err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	if repo.gotUserCall.baseID == nil || *repo.gotUserCall.baseID != 10 {
		t.Fatalf("baseID = %v, want 10", repo.gotUserCall.baseID)
	}
	if repo.gotUserCall.quoteID == nil || *repo.gotUserCall.quoteID != 20 {
		t.Fatalf("quoteID = %v, want 20", repo.gotUserCall.quoteID)
	}
}

func TestGetOrdersUnknownMarketReturnsErrMarketNotFound(t *testing.T) {
	svc := newTestService(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	_, err := svc.GetOrders(context.Background(), uuid.New(), orders.GetOrdersFilter{Market: "NOPE-NOPE"})
	if !errors.Is(err, orders.ErrMarketNotFound) {
		t.Fatalf("err = %v, want ErrMarketNotFound", err)
	}
}

func TestPublishOrderToQueueSuccess(t *testing.T) {
	pub := &fakePublisher{}
	svc := newTestService(&fakeOrderRepository{}, btcUsdtCache(), pub)
	userID := uuid.New()

	orderID, err := svc.PublishOrderToQueue(context.Background(), userID, &orders.OrderToPublish{
		MarketID: "BTC-USDT", Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 5,
	})
	if err != nil {
		t.Fatalf("PublishOrderToQueue: %v", err)
	}
	if orderID == nil {
		t.Fatal("orderID is nil")
	}
	if len(pub.calls) != 1 || pub.calls[0].marketRef != "BTC-USDT" {
		t.Fatalf("calls = %+v, want one publish to BTC-USDT", pub.calls)
	}
	open, err := pub.calls[0].event.DecodeOpenOrder()
	if err != nil {
		t.Fatalf("DecodeOpenOrder: %v", err)
	}
	if open.OrderID != *orderID || open.UserID != userID || open.MarketID != 1 || open.Price != 100 || open.Quantity != 5 {
		t.Fatalf("decoded event = %+v, unexpected shape", open)
	}
}

func TestPublishOrderToQueueUnknownMarket(t *testing.T) {
	svc := newTestService(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{})

	_, err := svc.PublishOrderToQueue(context.Background(), uuid.New(), &orders.OrderToPublish{
		MarketID: "NOPE-NOPE", Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 5,
	})
	if !errors.Is(err, orders.ErrMarketNotFound) {
		t.Fatalf("err = %v, want ErrMarketNotFound", err)
	}
}

func TestPublishOrderToQueueInvalidOrderRejectedBeforePublish(t *testing.T) {
	pub := &fakePublisher{}
	svc := newTestService(&fakeOrderRepository{}, btcUsdtCache(), pub)

	// Market order + GoodTillCancel is a nonsensical combination — see ValidateOrderEvent.
	_, err := svc.PublishOrderToQueue(context.Background(), uuid.New(), &orders.OrderToPublish{
		MarketID: "BTC-USDT", Side: oeq.BuyOrder, Type: oeq.MarketOrder, TimeInForce: oeq.GoodTillCancel,
	})
	if !errors.Is(err, orders.ErrInvalidOrder) {
		t.Fatalf("err = %v, want ErrInvalidOrder", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("calls = %d, want 0 (an invalid order must never reach the publisher)", len(pub.calls))
	}
}

func TestPublishOrderToQueuePublisherError(t *testing.T) {
	svc := newTestService(&fakeOrderRepository{}, btcUsdtCache(), &fakePublisher{err: errors.New("amqp: channel closed")})

	_, err := svc.PublishOrderToQueue(context.Background(), uuid.New(), &orders.OrderToPublish{
		MarketID: "BTC-USDT", Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 5,
	})
	if err == nil {
		t.Fatal("expected an error when the publisher fails")
	}
}

func TestCancelOrderSuccess(t *testing.T) {
	repo := &fakeOrderRepository{orderByID: &repository.OrderRow{
		HaveInstrumentID: 10, WantInstrumentID: 20,
	}}
	pub := &fakePublisher{}
	svc := newTestService(repo, btcUsdtCache(), pub)
	orderID := uuid.New()

	if err := svc.CancelOrder(context.Background(), uuid.New(), orderID); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if len(pub.calls) != 1 || pub.calls[0].marketRef != "BTC-USDT" {
		t.Fatalf("calls = %+v, want one publish to BTC-USDT", pub.calls)
	}
	cancel, err := pub.calls[0].event.DecodeCancelOrder()
	if err != nil {
		t.Fatalf("DecodeCancelOrder: %v", err)
	}
	if cancel.OrderID != orderID || cancel.MarketRef != "BTC-USDT" {
		t.Fatalf("decoded event = %+v, unexpected shape", cancel)
	}
}

func TestCancelOrderNotFound(t *testing.T) {
	repo := &fakeOrderRepository{orderByIDErr: repository.ErrOrderNotFound}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	err := svc.CancelOrder(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, orders.ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

func TestBatchCancelOrdersMixedResults(t *testing.T) {
	knownID := uuid.New()
	unknownID := uuid.New()
	repo := &fakeOrderRepository{ordersByIDs: []repository.OrderRow{
		{ID: knownID, HaveInstrumentID: 10, WantInstrumentID: 20},
	}}
	pub := &fakePublisher{}
	svc := newTestService(repo, btcUsdtCache(), pub)

	results, err := svc.BatchCancelOrders(context.Background(), uuid.New(), []uuid.UUID{knownID, unknownID})
	if err != nil {
		t.Fatalf("BatchCancelOrders: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].OrderID != knownID || results[0].Err != nil {
		t.Fatalf("results[0] = %+v, want a successful cancel of %v", results[0], knownID)
	}
	if results[1].OrderID != unknownID || !errors.Is(results[1].Err, orders.ErrOrderNotFound) {
		t.Fatalf("results[1] = %+v, want ErrOrderNotFound for %v", results[1], unknownID)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (only the known order publishes a cancel)", len(pub.calls))
	}
}

func TestBatchCancelOrdersRepositoryErrorFailsWholeBatch(t *testing.T) {
	repo := &fakeOrderRepository{ordersByIDsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo, btcUsdtCache(), &fakePublisher{})

	_, err := svc.BatchCancelOrders(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})
	if err == nil {
		t.Fatal("expected an error when the repository lookup fails")
	}
}
