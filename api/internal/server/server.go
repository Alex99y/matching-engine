package server

import (
	"context"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/api/internal/instruments"
	"github.com/alex99y/matching-engine/api/internal/markets"
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
	Logger *logger.Logger
	// Metrics       *metrics.ApiMetrics
	UsersHandler       *users.UserHandler
	InstrumentsHandler *instruments.InstrumentHandler
	MarketsHandler     *markets.MarketHandler
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
	if dependencies.UsersHandler == nil {
		panic("user handler cannot be nil")
	}
	if dependencies.InstrumentsHandler == nil {
		panic("instruments handler cannot be nil")
	}
	if dependencies.MarketsHandler == nil {
		panic("markets handler cannot be nil")
	}

	app := fiber.New(fiber.Config{
		StructValidator: validations.NewStructValidator(),
	})
	app.Use(middleware.AccessLog(dependencies.Logger))
	app.Use(requestid.New(requestid.Config{
		Generator: func() string {
			return uuid.New().String()
		},
	}))

	// TODO: Configure limiter
	app.Use(limiter.New(limiter.Config{
		Max:        100,
		Expiration: 1 * time.Minute,
	}))

	app.Get("/health", healthcheck.New())
	apiV1 := app.Group("/api/v1")
	users.RegisterUserRoutes(apiV1, dependencies.UsersHandler)
	instruments.RegisterInstrumentRoutes(apiV1, dependencies.InstrumentsHandler)
	markets.RegisterMarketRoutes(apiV1, dependencies.MarketsHandler)

	return &Server{httpServer: app}
}
