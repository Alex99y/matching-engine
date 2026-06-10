package orders

import (
	"errors"
	"strconv"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/google/uuid"
)

type CreateOrderRequest struct {
	ClientOrderID string                         `json:"client_order_id"    validate:"omitempty,min=32,max=64"`
	OrderSide     order_events_queue.OrderSide   `json:"order_side"         validate:"required"`
	OrderType     order_events_queue.OrderType   `json:"order_type"         validate:"required"`
	TimeInForce   order_events_queue.TimeInForce `json:"order_tif"          validate:"required"`
	Market        string                         `json:"market"          validate:"required"`
	Price         uint64                         `json:"price"`
	Quantity      uint64                         `json:"quantity"`
	QuoteQty      *uint64                        `json:"quote_qty,omitempty"`
	ExpiresAt     *int64                         `json:"expires_at,omitempty"`
}

type CreateOrderResponse struct {
	OrderID uuid.UUID `json:"order_id"`
}

type OpenOrder struct {
	Price         uint64 `json:"price"`
	Side          string `json:"side"`
	RemainingHave uint64 `json:"remaining_have"`
	RemainingWant uint64 `json:"remaining_want"`
}

type CancelledOrder struct {
	CancelledAt   int64  `json:"cancelled_at"`
	RemainingHave uint64 `json:"remaining_have"`
	RemainingWant uint64 `json:"remaining_want"`
}

type OrderResponse struct {
	ID             uuid.UUID       `json:"id"`
	ClientOrderID  string          `json:"client_order_id,omitempty"`
	Type           string          `json:"type"`
	TimeInForce    string          `json:"time_in_force"`
	HaveQuantity   uint64          `json:"have_quantity"`
	WantQuantity   uint64          `json:"want_quantity"`
	CreatedAt      int64           `json:"created_at"`
	ExpiresAt      *int64          `json:"expires_at,omitempty"`
	OpenOrder      *OpenOrder      `json:"open_order,omitempty"`
	CancelledOrder *CancelledOrder `json:"cancelled_order,omitempty"`
}

func orderRowToResponse(row *repository.OrderRow) OrderResponse {
	resp := OrderResponse{
		ID:            row.ID,
		ClientOrderID: row.ClientOrderID,
		Type:          row.Type,
		TimeInForce:   row.TimeInForce,
		HaveQuantity:  row.HaveQuantity,
		WantQuantity:  row.WantQuantity,
		CreatedAt:     row.CreatedAt,
		ExpiresAt:     row.ExpiresAt,
	}

	if row.Price != nil {
		resp.OpenOrder = &OpenOrder{
			Price:         *row.Price,
			Side:          *row.Side,
			RemainingHave: *row.ORemainingHaveAmount,
			RemainingWant: *row.ORemainingWantAmount,
		}
	}

	if row.CancelledAt != nil {
		resp.CancelledOrder = &CancelledOrder{
			CancelledAt:   *row.CancelledAt,
			RemainingHave: *row.CRemainingHaveAmount,
			RemainingWant: *row.CRemainingWantAmount,
		}
	}

	return resp
}

type OrderHandler struct {
	logger       *logger.Logger
	orderService *OrderService
}

func (o *OrderHandler) GetOrder(c fiber.Ctx) error {
	orderID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid order id")
	}

	userID := middleware.UserIDFromContext(c)

	order, err := o.orderService.GetOrderByID(c.Context(), userID, orderID)
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "order not found")
		}
		return utils.NewServerErrorResponse(c, o.logger, err)
	}

	return c.JSON(orderRowToResponse(order))
}

func (o *OrderHandler) GetOrders(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)

	filter := GetOrdersFilter{
		ClientOrderID:       c.Query("client_order_id"),
		Market:              c.Query("market"),
		ShowOpenOrders:      c.Query("show_open") == "true",
		ShowCancelledOrders: c.Query("show_cancelled") == "true",
	}

	if raw := c.Query("start_date"); raw != "" {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
		}
		filter.StartDate = &t
	}

	if raw := c.Query("end_date"); raw != "" {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid end_date, expected YYYY-MM-DD")
		}
		filter.EndDate = &t
	}

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid limit, must be a positive integer")
		}
		filter.Limit = n
	}

	orders, err := o.orderService.GetOrders(c.Context(), userID, filter)
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "order not found")
		}
		if errors.Is(err, ErrMarketNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "market not found")
		}
		if errors.Is(err, ErrInvalidLimit) {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "limit must be between 1 and 100")
		}
		return utils.NewServerErrorResponse(c, o.logger, err)
	}

	response := make([]OrderResponse, len(orders))
	for i := range orders {
		response[i] = orderRowToResponse(&orders[i])
	}
	return c.JSON(response)
}

func (o *OrderHandler) CreateOrder(c fiber.Ctx) error {
	var req CreateOrderRequest
	if err := c.Bind().Body(&req); err != nil {
		o.logger.Error("CreateOrder: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	userID := middleware.UserIDFromContext(c)

	orderID, err := o.orderService.PublishOrderToQueue(
		c.Context(),
		userID,
		&OrderToPublish{
			ClientOrderID: req.ClientOrderID,
			MarketID:      req.Market,
			Side:          req.OrderSide,
			Type:          req.OrderType,
			TimeInForce:   req.TimeInForce,
			Price:         req.Price,
			Quantity:      req.Quantity,
			QuoteQty:      req.QuoteQty,
			ExpiresAt:     req.ExpiresAt,
		},
	)
	if err != nil {
		if errors.Is(err, ErrMarketNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "market not found")
		}
		if errors.Is(err, ErrInvalidOrder) {
			return utils.NewErrorResponse(c, fiber.StatusUnprocessableEntity, "invalid order")
		}
		return utils.NewServerErrorResponse(c, o.logger, err)
	}

	return c.Status(fiber.StatusAccepted).JSON(CreateOrderResponse{OrderID: *orderID})
}

func (o *OrderHandler) CancelOrder(c fiber.Ctx) error {
	orderID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid order id")
	}

	userID := middleware.UserIDFromContext(c)

	if err := o.orderService.CancelOrder(c.Context(), userID, orderID); err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return utils.NewErrorResponse(c, fiber.StatusNotFound, "order not found")
		}
		return utils.NewServerErrorResponse(c, o.logger, err)
	}

	return c.SendStatus(fiber.StatusAccepted)
}

func NewOrderHandler(logger *logger.Logger, orderService *OrderService) *OrderHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if orderService == nil {
		panic("order service cannot be nil")
	}

	return &OrderHandler{
		logger:       logger,
		orderService: orderService,
	}
}
