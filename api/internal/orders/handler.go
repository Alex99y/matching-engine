package orders

import (
	"errors"
	"fmt"
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

const MaxBatchSize = 500

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

type BatchCreateOrderResult struct {
	Index   int        `json:"index"`
	OrderID *uuid.UUID `json:"order_id,omitempty"`
	Error   *string    `json:"error,omitempty"`
}

type BatchCreateOrderResponse struct {
	Results []BatchCreateOrderResult `json:"results"`
}

type BatchCancelOrderRequest struct {
	OrderIDs []string `json:"order_ids"`
}

type BatchCancelOrderResult struct {
	OrderID string  `json:"order_id"`
	Error   *string `json:"error,omitempty"`
}

type BatchCancelOrderResponse struct {
	Results []BatchCancelOrderResult `json:"results"`
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

	if row.Price != nil && row.Side != nil &&
		row.ORemainingHaveAmount != nil && row.ORemainingWantAmount != nil {
		resp.OpenOrder = &OpenOrder{
			Price:         *row.Price,
			Side:          *row.Side,
			RemainingHave: *row.ORemainingHaveAmount,
			RemainingWant: *row.ORemainingWantAmount,
		}
	}

	if row.CancelledAt != nil &&
		row.CRemainingHaveAmount != nil && row.CRemainingWantAmount != nil {
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
	var reqs []CreateOrderRequest
	if err := c.Bind().Body(&reqs); err != nil {
		o.logger.Error("CreateOrder: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	if len(reqs) == 0 {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "request body must be a non-empty array")
	}
	if len(reqs) > MaxBatchSize {
		return utils.NewErrorResponse(c, fiber.StatusUnprocessableEntity, fmt.Sprintf("batch size exceeds maximum of %d", MaxBatchSize))
	}

	userID := middleware.UserIDFromContext(c)
	results := make([]BatchCreateOrderResult, len(reqs))

	for i, req := range reqs {
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
			var errStr string
			switch {
			case errors.Is(err, ErrMarketNotFound):
				errStr = "market not found"
			case errors.Is(err, ErrInvalidOrder):
				errStr = "invalid order"
			default:
				o.logger.Error(fmt.Sprintf("CreateOrder: index %d: %v, request_id=%s", i, err, requestid.FromContext(c)))
				errStr = "internal error"
			}
			results[i] = BatchCreateOrderResult{Index: i, Error: &errStr}
		} else {
			results[i] = BatchCreateOrderResult{Index: i, OrderID: orderID}
		}
	}

	return c.Status(fiber.StatusAccepted).JSON(BatchCreateOrderResponse{Results: results})
}

func (o *OrderHandler) CancelOrder(c fiber.Ctx) error {
	var req BatchCancelOrderRequest
	if err := c.Bind().Body(&req); err != nil {
		o.logger.Error("CancelOrder: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	if len(req.OrderIDs) == 0 {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "order_ids must be a non-empty array")
	}
	if len(req.OrderIDs) > MaxBatchSize {
		return utils.NewErrorResponse(c, fiber.StatusUnprocessableEntity, fmt.Sprintf("batch size exceeds maximum of %d", MaxBatchSize))
	}

	orderIDs := make([]uuid.UUID, 0, len(req.OrderIDs))
	for _, raw := range req.OrderIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, fmt.Sprintf("invalid order id: %s", raw))
		}
		orderIDs = append(orderIDs, id)
	}

	userID := middleware.UserIDFromContext(c)

	cancelResults, err := o.orderService.BatchCancelOrders(c.Context(), userID, orderIDs)
	if err != nil {
		return utils.NewServerErrorResponse(c, o.logger, err)
	}

	results := make([]BatchCancelOrderResult, len(cancelResults))
	for i, r := range cancelResults {
		results[i] = BatchCancelOrderResult{OrderID: r.OrderID.String()}
		if r.Err != nil {
			var errStr string
			switch {
			case errors.Is(r.Err, ErrOrderNotFound):
				errStr = "order not found"
			case errors.Is(r.Err, ErrMarketNotFound):
				errStr = "market not found"
			default:
				o.logger.Error(fmt.Sprintf("CancelOrder: index %d: %v, request_id=%s", i, r.Err, requestid.FromContext(c)))
				errStr = "internal error"
			}
			results[i].Error = &errStr
		}
	}

	return c.Status(fiber.StatusAccepted).JSON(BatchCancelOrderResponse{Results: results})
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
