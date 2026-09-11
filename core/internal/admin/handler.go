package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// adminService is the subset of Service the handler needs.
type adminService interface {
	Pause(ctx context.Context, marketRef string) error
	Resume(ctx context.Context, marketRef string) error
	Status(ctx context.Context) []MarketStatus
	ListUserOrders(ctx context.Context, username string, includeOpen, includeCancelled bool) (*UserOrders, error)
	CancelUserOrders(ctx context.Context, username string, orderIDs []uuid.UUID, all bool) (*CancelSummary, error)
}

type cancelUserOrdersRequest struct {
	OrderIDs []string `json:"order_ids"`
	All      bool     `json:"all"`
}

type Handler struct {
	service adminService
	logger  *logger.Logger
}

func NewHandler(service adminService, log *logger.Logger) *Handler {
	if service == nil {
		panic("admin service cannot be nil")
	}
	if log == nil {
		panic("logger cannot be nil")
	}
	return &Handler{service: service, logger: log}
}

func (h *Handler) Pause(c fiber.Ctx) error {
	return h.setPaused(c, true)
}

func (h *Handler) Resume(c fiber.Ctx) error {
	return h.setPaused(c, false)
}

func (h *Handler) setPaused(c fiber.Ctx, paused bool) error {
	marketRef := c.Params("market")

	var err error
	if paused {
		err = h.service.Pause(c.Context(), marketRef)
	} else {
		err = h.service.Resume(c.Context(), marketRef)
	}

	switch {
	case errors.Is(err, ErrMarketNotServed):
		return utils.NewErrorResponse(c, fiber.StatusNotFound, "market not served by this core")
	case err != nil:
		h.logger.Error(fmt.Sprintf("admin: set paused=%v for %q: %v", paused, marketRef, err))
		return utils.NewErrorResponse(c, fiber.StatusInternalServerError, "internal server error")
	}

	// Logged at WARN because a halted market is an incident-shaped event an operator will look for
	// in the log afterwards, not routine traffic.
	h.logger.Warn(fmt.Sprintf("admin: market %s %s", marketRef, verb(paused)))
	return c.JSON(MarketStatus{Market: marketRef, Paused: paused})
}

func (h *Handler) Status(c fiber.Ctx) error {
	return c.JSON(h.service.Status(c.Context()))
}

// ListUserOrders defaults to open orders only — those are the ones an operator can act on. Pass
// ?cancelled=true for history, ?open=false to see only cancelled.
func (h *Handler) ListUserOrders(c fiber.Ctx) error {
	username := c.Params("username")
	includeOpen := c.Query("open") != "false"
	includeCancelled := c.Query("cancelled") == "true"

	orders, err := h.service.ListUserOrders(c.Context(), username, includeOpen, includeCancelled)
	if err != nil {
		return h.userError(c, username, "list orders", err)
	}
	return c.JSON(orders)
}

func (h *Handler) CancelUserOrders(c fiber.Ctx) error {
	username := c.Params("username")

	var req cancelUserOrdersRequest
	if err := c.Bind().Body(&req); err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	orderIDs := make([]uuid.UUID, 0, len(req.OrderIDs))
	for _, raw := range req.OrderIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, fmt.Sprintf("invalid order id %q", raw))
		}
		orderIDs = append(orderIDs, id)
	}

	summary, err := h.service.CancelUserOrders(c.Context(), username, orderIDs, req.All)
	if err != nil {
		return h.userError(c, username, "cancel orders", err)
	}

	h.logger.Warn(fmt.Sprintf("admin: cancelled orders for user %s — %d queued, %d skipped",
		username, summary.Queued, summary.Skipped))
	return c.JSON(summary)
}

func (h *Handler) userError(c fiber.Ctx, username, op string, err error) error {
	switch {
	case errors.Is(err, ErrUserNotFound):
		return utils.NewErrorResponse(c, fiber.StatusNotFound, "user not found")
	case errors.Is(err, ErrUserNotFrozen):
		return utils.NewErrorResponse(c, fiber.StatusConflict,
			"user must be frozen first: run `cli user freeze --username "+username+"`")
	case errors.Is(err, ErrNoTarget):
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "specify order_ids or all")
	default:
		h.logger.Error(fmt.Sprintf("admin: %s for %q: %v", op, username, err))
		return utils.NewErrorResponse(c, fiber.StatusInternalServerError, "internal server error")
	}
}

func verb(paused bool) string {
	if paused {
		return "PAUSED"
	}
	return "RESUMED"
}
