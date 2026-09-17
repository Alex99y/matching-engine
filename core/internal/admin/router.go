package admin

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

var ErrMissingToken = errors.New("admin token is required: set ADMIN_TOKEN")

const bearerPrefix = "Bearer "

// Server is core's admin listener. It is separate from the Prometheus server on purpose: the metrics
// port is open to scraping, and these routes stop a market from trading.
type Server struct {
	port      int
	app       *fiber.App
	logger    *logger.Logger
	isRunning bool
}

// NewServer wires the routes behind bearer-token auth. An empty token is rejected here rather than
// defaulted, so a misconfigured deployment fails to boot instead of serving an anonymous halt button.
func NewServer(port int, token string, handler *Handler, log *logger.Logger) (*Server, error) {
	if token == "" {
		return nil, ErrMissingToken
	}

	app := fiber.New()
	RegisterAdminRoutes(app, token, handler)

	return &Server{port: port, app: app, logger: log}, nil
}

// RegisterAdminRoutes mounts the circuit-breaker routes behind bearer-token auth.
func RegisterAdminRoutes(app fiber.Router, token string, handler *Handler) {
	admin := app.Group("/admin", requireToken(token))
	admin.Get("/markets", handler.Status)
	admin.Post("/markets/:market/pause", handler.Pause)
	admin.Post("/markets/:market/resume", handler.Resume)
	admin.Get("/users/:username/orders", handler.ListUserOrders)
	admin.Post("/users/:username/orders/cancel", handler.CancelUserOrders)
	admin.Get("/dlq", handler.ListDeadLetters)
}

// requireToken compares in constant time so a caller cannot recover the token by timing its guesses.
func requireToken(token string) fiber.Handler {
	want := []byte(token)
	return func(c fiber.Ctx) error {
		got := strings.TrimPrefix(c.Get(fiber.HeaderAuthorization), bearerPrefix)
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "unauthorized")
		}
		return c.Next()
	}
}

func (s *Server) Start() error {
	if s.isRunning {
		return nil
	}

	address := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("admin server bind %s: %w", address, err)
	}

	s.isRunning = true
	s.logger.Info(fmt.Sprintf("starting admin server on %s", listener.Addr().String()))

	go func() {
		if err := s.app.Listener(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error(fmt.Sprintf("admin server stopped with error: %v", err))
		}
		s.isRunning = false
	}()

	return nil
}

func (s *Server) Stop() error {
	if !s.isRunning {
		return nil
	}
	s.isRunning = false
	return s.app.Shutdown()
}
