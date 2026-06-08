package orders

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
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
	ShowOpenOrders      bool
	ShowCancelledOrders bool
}

type CacheService interface {
	GetMarketByRef(marketRef string) (*repository.Market, error)
}

type OrderRepository interface {
	GetOrderByID(ctx context.Context, userID uuid.UUID, id uuid.UUID) (*repository.OrderRow, error)
	GetOrderByClientOrderID(ctx context.Context, userID uuid.UUID, clientOrderID string) (*repository.OrderRow, error)
	GetOrdersByUser(ctx context.Context, userID uuid.UUID, showOpenOrders bool, showCancelledOrders bool) ([]repository.OrderRow, error)
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

	orders, err := o.orderRepository.GetOrdersByUser(ctx, userID, filter.ShowOpenOrders, filter.ShowCancelledOrders)
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

	orderEvent := &order_events_queue.OrderEvent{
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
		orderEvent,
		order_events_queue.MarketConstraints{
			PriceQuantum:  uint64(market.PriceQuantum),
			AmountQuantum: uint64(market.AmountQuantum),
			MinOrderSize:  uint64(market.MinOrderSize),
			MaxOrderSize:  uint64(market.MaxOrderSize),
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidOrder, err)
	}

	if err := o.publisher.Publish(ctx, order.MarketID, orderEvent); err != nil {
		return nil, fmt.Errorf("publish order event: %w", err)
	}

	return &orderID, nil
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
