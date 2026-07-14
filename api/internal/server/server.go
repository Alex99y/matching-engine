package server

import (
	"context"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/api/internal/instruments"
	"github.com/alex99y/matching-engine/api/internal/markets"
	"github.com/alex99y/matching-engine/api/internal/metrics"
	"github.com/alex99y/matching-engine/api/internal/orders"
	"github.com/alex99y/matching-engine/api/internal/sessions"
	"github.com/alex99y/matching-engine/api/internal/stream"
	"github.com/alex99y/matching-engine/api/internal/users"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/google/uuid"
)

type ServerDependencies struct {
	Logger             *logger.Logger
	AuthMiddleware     middleware.AuthMiddleware
	Metrics            *metrics.ApiMetrics
	SessionsHandler    *sessions.SessionHandler
	UsersHandler       *users.UserHandler
	InstrumentsHandler *instruments.InstrumentHandler
	MarketsHandler     *markets.MarketHandler
	OrdersHandler      *orders.OrderHandler
	StreamHandler      *stream.StreamHandler
}

type Server struct {
	httpServer *fiber.App
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.ShutdownWithContext(ctx)
}

func (s *Server) Start(port int, host string) error {
	return s.httpServer.Listen(fmt.Sprintf("%s:%d", host, port))
}

func NewServer(dependencies ServerDependencies) *Server {
	if dependencies.Logger == nil {
		panic("logger cannot be nil")
	}
	if dependencies.AuthMiddleware == nil {
		panic("auth middleware cannot be nil")
	}
	if dependencies.SessionsHandler == nil {
		panic("sessions handler cannot be nil")
	}
	if dependencies.UsersHandler == nil {
		panic("user handler cannot be nil")
	}
	if dependencies.InstrumentsHandler == nil {
		panic("instruments handler cannot be nil")
	}
	if dependencies.MarketsHandler == nil {
		panic("markets handler cannot be nil")
	}
	if dependencies.OrdersHandler == nil {
		panic("orders handler cannot be nil")
	}
	if dependencies.StreamHandler == nil {
		panic("stream handler cannot be nil")
	}

	app := fiber.New(fiber.Config{
		StructValidator: validations.NewStructValidator(),
	})
	app.Use(middleware.AccessLog(dependencies.Logger, dependencies.Metrics))
	app.Use(requestid.New(requestid.Config{
		Generator: func() string {
			return uuid.New().String()
		},
	}))

	// TODO: Configure limiter
	app.Use(limiter.New(limiter.Config{
		Max:        60000,
		Expiration: 1 * time.Minute,
	}))

	app.Get("/health", healthcheck.New())
	apiV1 := app.Group("/api/v1")
	sessions.RegisterSessionRoutes(apiV1, dependencies.AuthMiddleware, dependencies.SessionsHandler)
	users.RegisterUserRoutes(apiV1, dependencies.AuthMiddleware, dependencies.UsersHandler)
	instruments.RegisterInstrumentRoutes(apiV1, dependencies.InstrumentsHandler)
	markets.RegisterMarketRoutes(apiV1, dependencies.MarketsHandler)
	orders.RegisterOrderRoutes(apiV1, dependencies.AuthMiddleware, dependencies.OrdersHandler)
	stream.RegisterStreamRoutes(apiV1, dependencies.AuthMiddleware, dependencies.StreamHandler)

	return &Server{httpServer: app}
}
