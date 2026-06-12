package orders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/uuidv7"
	"github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

var (
	ErrMarketNotFound = errors.New("market not found")
	ErrInvalidOrder   = errors.New("invalid order")
	ErrCreatingUUID   = errors.New("could not create uuid for order")
	ErrOrderNotFound  = errors.New("order not found")
	ErrInvalidLimit   = errors.New("limit must be between 1 and 100")
)

type OrderToPublish struct {
	ClientOrderID string
	MarketID      string
	Side          order_events_queue.OrderSide
	Type          order_events_queue.OrderType
	TimeInForce   order_events_queue.TimeInForce
	Price         uint64
	Quantity      uint64
	QuoteQty      *uint64
	ExpiresAt     *int64
}

type GetOrdersFilter struct {
	ClientOrderID       string
	Market              string
	StartDate           *time.Time
	EndDate             *time.Time
	Limit               int
	ShowOpenOrders      bool
	ShowCancelledOrders bool
}

type CacheService interface {
	GetMarketByRef(marketRef string) (*repository.Market, error)
	GetInstrumentByID(id int) (*repository.Instrument, error)
}

type OrderRepository interface {
	GetOrderByID(ctx context.Context, userID uuid.UUID, id uuid.UUID) (*repository.OrderRow, error)
	GetOrderByClientOrderID(ctx context.Context, userID uuid.UUID, clientOrderID string) (*repository.OrderRow, error)
	GetOrdersByUser(ctx context.Context, userID uuid.UUID, showOpenOrders bool, showCancelledOrders bool, baseInstrumentID, quoteInstrumentID *int, startDate, endDate *time.Time, limit int) ([]repository.OrderRow, error)
	GetOrdersByIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]repository.OrderRow, error)
}

type BatchCancelResult struct {
	OrderID uuid.UUID
	Err     error
}

type OrderCommandPublisher interface {
	Publish(ctx context.Context, marketRef string, event *order_events_queue.OrderEvent) error
}

type OrderService struct {
	logger          *logger.Logger
	orderRepository OrderRepository
	cacheService    CacheService
	publisher       OrderCommandPublisher
}

func (o *OrderService) GetOrderByID(ctx context.Context, userID uuid.UUID, id uuid.UUID) (*repository.OrderRow, error) {
	order, err := o.orderRepository.GetOrderByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrOrderNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("get order by id: %w", err)
	}

	return order, nil
}

func (o *OrderService) GetOrders(ctx context.Context, userID uuid.UUID, filter GetOrdersFilter) ([]repository.OrderRow, error) {
	if filter.ClientOrderID != "" {
		order, err := o.orderRepository.GetOrderByClientOrderID(ctx, userID, filter.ClientOrderID)
		if err != nil {
			if errors.Is(err, repository.ErrOrderNotFound) {
				return nil, ErrOrderNotFound
			}
			return nil, fmt.Errorf("get orders: %w", err)
		}
		return []repository.OrderRow{*order}, nil
	}

	limit := filter.Limit
	if limit == 0 {
		limit = 10
	} else if limit > 100 {
		return nil, ErrInvalidLimit
	}

	var baseInstrumentID, quoteInstrumentID *int
	if filter.Market != "" {
		market, err := o.cacheService.GetMarketByRef(filter.Market)
		if err != nil {
			return nil, ErrMarketNotFound
		}
		baseInstrumentID = &market.BaseInstrumentID
		quoteInstrumentID = &market.QuoteInstrumentID
	}

	orders, err := o.orderRepository.GetOrdersByUser(ctx, userID, filter.ShowOpenOrders, filter.ShowCancelledOrders, baseInstrumentID, quoteInstrumentID, filter.StartDate, filter.EndDate, limit)
	if err != nil {
		return nil, fmt.Errorf("get orders: %w", err)
	}
	return orders, nil
}

func (o *OrderService) PublishOrderToQueue(
	ctx context.Context,
	userID uuid.UUID,
	order *OrderToPublish,
) (*uuid.UUID, error) {
	market, err := o.cacheService.GetMarketByRef(order.MarketID)
	if err != nil {
		return nil, ErrMarketNotFound
	}

	orderID, err := uuidv7.New()
	if err != nil {
		return nil, ErrCreatingUUID
	}

	openEvent := &order_events_queue.OpenOrderEvent{
		OrderID:       orderID,
		UserID:        userID,
		ClientOrderID: order.ClientOrderID,
		Side:          order.Side,
		Type:          order.Type,
		TimeInForce:   order.TimeInForce,
		MarketID:      market.ID,
		Price:         order.Price,
		Quantity:      order.Quantity,
		QuoteQty:      order.QuoteQty,
		ExpiresAt:     order.ExpiresAt,
	}

	if err := order_events_queue.ValidateOrderEvent(
		openEvent,
		order_events_queue.MarketConstraints{
			PriceQuantum:  uint64(market.PriceQuantum),
			AmountQuantum: uint64(market.AmountQuantum),
			MinOrderSize:  uint64(market.MinOrderSize),
			MaxOrderSize:  uint64(market.MaxOrderSize),
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidOrder, err)
	}

	event, err := order_events_queue.NewOpenOrderEvent(openEvent)
	if err != nil {
		return nil, fmt.Errorf("create open order event: %w", err)
	}

	if err := o.publisher.Publish(ctx, order.MarketID, event); err != nil {
		return nil, fmt.Errorf("publish order event: %w", err)
	}

	return &orderID, nil
}

func (o *OrderService) CancelOrder(ctx context.Context, userID uuid.UUID, orderID uuid.UUID) error {
	order, err := o.orderRepository.GetOrderByID(ctx, userID, orderID)
	if err != nil {
		if errors.Is(err, repository.ErrOrderNotFound) {
			return ErrOrderNotFound
		}
		return fmt.Errorf("cancel order: get order: %w", err)
	}

	haveInstr, err := o.cacheService.GetInstrumentByID(order.HaveInstrumentID)
	if err != nil {
		return fmt.Errorf("cancel order: resolve have instrument: %w", err)
	}
	wantInstr, err := o.cacheService.GetInstrumentByID(order.WantInstrumentID)
	if err != nil {
		return fmt.Errorf("cancel order: resolve want instrument: %w", err)
	}

	// Sell side: have=base, want=quote → "BTC-USDT"
	// Buy side:  have=quote, want=base → "USDT-BTC" → try inverted → "BTC-USDT"
	marketRef := utils.MergeMarketRef(haveInstr.Symbol, wantInstr.Symbol)
	if _, err := o.cacheService.GetMarketByRef(marketRef); err != nil {
		marketRef = utils.MergeMarketRef(wantInstr.Symbol, haveInstr.Symbol)
		if _, err := o.cacheService.GetMarketByRef(marketRef); err != nil {
			return ErrMarketNotFound
		}
	}

	cancelEvent := &order_events_queue.CancelOrderEvent{
		OrderID:   orderID,
		MarketRef: marketRef,
	}
	event, err := order_events_queue.NewCancelOrderEvent(cancelEvent)
	if err != nil {
		return fmt.Errorf("cancel order: create event: %w", err)
	}

	if err := o.publisher.Publish(ctx, marketRef, event); err != nil {
		return fmt.Errorf("cancel order: publish: %w", err)
	}

	return nil
}

func (o *OrderService) BatchCancelOrders(ctx context.Context, userID uuid.UUID, orderIDs []uuid.UUID) ([]BatchCancelResult, error) {
	orders, err := o.orderRepository.GetOrdersByIDs(ctx, userID, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("batch cancel orders: %w", err)
	}

	found := make(map[uuid.UUID]*repository.OrderRow, len(orders))
	for i := range orders {
		found[orders[i].ID] = &orders[i]
	}

	results := make([]BatchCancelResult, len(orderIDs))
	for i, orderID := range orderIDs {
		order, ok := found[orderID]
		if !ok {
			results[i] = BatchCancelResult{OrderID: orderID, Err: ErrOrderNotFound}
			continue
		}

		haveInstr, err := o.cacheService.GetInstrumentByID(order.HaveInstrumentID)
		if err != nil {
			results[i] = BatchCancelResult{OrderID: orderID, Err: fmt.Errorf("batch cancel: resolve have instrument: %w", err)}
			continue
		}
		wantInstr, err := o.cacheService.GetInstrumentByID(order.WantInstrumentID)
		if err != nil {
			results[i] = BatchCancelResult{OrderID: orderID, Err: fmt.Errorf("batch cancel: resolve want instrument: %w", err)}
			continue
		}

		marketRef := utils.MergeMarketRef(haveInstr.Symbol, wantInstr.Symbol)
		if _, err := o.cacheService.GetMarketByRef(marketRef); err != nil {
			marketRef = utils.MergeMarketRef(wantInstr.Symbol, haveInstr.Symbol)
			if _, err := o.cacheService.GetMarketByRef(marketRef); err != nil {
				results[i] = BatchCancelResult{OrderID: orderID, Err: ErrMarketNotFound}
				continue
			}
		}

		cancelEvent := &order_events_queue.CancelOrderEvent{
			OrderID:   orderID,
			MarketRef: marketRef,
		}
		event, err := order_events_queue.NewCancelOrderEvent(cancelEvent)
		if err != nil {
			results[i] = BatchCancelResult{OrderID: orderID, Err: fmt.Errorf("batch cancel: create event: %w", err)}
			continue
		}

		if err := o.publisher.Publish(ctx, marketRef, event); err != nil {
			results[i] = BatchCancelResult{OrderID: orderID, Err: fmt.Errorf("batch cancel: publish: %w", err)}
			continue
		}

		results[i] = BatchCancelResult{OrderID: orderID}
	}

	return results, nil
}

func NewOrderService(
	logger *logger.Logger,
	orderRepository OrderRepository,
	cacheService CacheService,
	publisher OrderCommandPublisher,
) *OrderService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if orderRepository == nil {
		panic("order repository cannot be nil")
	}
	if cacheService == nil {
		panic("cache service cannot be nil")
	}
	if publisher == nil {
		panic("publisher cannot be nil")
	}

	return &OrderService{
		logger:          logger,
		orderRepository: orderRepository,
		cacheService:    cacheService,
		publisher:       publisher,
	}
}
