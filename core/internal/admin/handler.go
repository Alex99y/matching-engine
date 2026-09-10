package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

// adminService is the subset of Service the handler needs.
type adminService interface {
	Pause(ctx context.Context, marketRef string) error
	Resume(ctx context.Context, marketRef string) error
	Status(ctx context.Context) []MarketStatus
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
		return errorResponse(c, fiber.StatusNotFound, "market not served by this core")
	case err != nil:
		h.logger.Error(fmt.Sprintf("admin: set paused=%v for %q: %v", paused, marketRef, err))
		return errorResponse(c, fiber.StatusInternalServerError, "internal server error")
	}

	// Logged at WARN because a halted market is an incident-shaped event an operator will look for
	// in the log afterwards, not routine traffic.
	h.logger.Warn(fmt.Sprintf("admin: market %s %s", marketRef, verb(paused)))
	return c.JSON(MarketStatus{Market: marketRef, Paused: paused})
}

func (h *Handler) Status(c fiber.Ctx) error {
	return c.JSON(h.service.Status(c.Context()))
}

func verb(paused bool) string {
	if paused {
		return "PAUSED"
	}
	return "RESUMED"
}

func errorResponse(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"message": message})
}
