package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/api/internal/config"
	"github.com/alex99y/matching-engine/api/internal/instruments"
	"github.com/alex99y/matching-engine/api/internal/markets"
	"github.com/alex99y/matching-engine/api/internal/metrics"
	"github.com/alex99y/matching-engine/api/internal/orders"
	"github.com/alex99y/matching-engine/api/internal/server"
	"github.com/alex99y/matching-engine/api/internal/sessions"
	"github.com/alex99y/matching-engine/api/internal/stream"
	"github.com/alex99y/matching-engine/api/internal/users"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/orderqueue"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	"github.com/alex99y/matching-engine/common/pkg/os"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/cache"
	dbmetrics "github.com/alex99y/matching-engine/db/pkg/metrics"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func main() {
	apiConfig := config.NewApiConfig()
	log := logger.NewLogger(apiConfig.DebugLevel)

	metricsRegistry := observability.NewServiceRegistry()
	apiSubsystem := observability.NewSubsystemMetrics(metricsRegistry, "me", "api")
	apiMetrics, err := metrics.NewApiMetrics(apiSubsystem)
	if err != nil {
		panic(err)
	}
	dbSubsystem := observability.NewSubsystemMetrics(metricsRegistry, "me", "db")
	metricsServer := observability.NewPrometheusServer(apiConfig.MetricsPort, apiSubsystem, log)
	if err := metricsServer.Start(); err != nil {
		panic(err)
	}
	defer func() {
		if err := metricsServer.Stop(); err != nil {
			log.Error(fmt.Sprintf("stopping metrics server: %v", err))
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	postgresqlClient, err := postgres.Connect(
		ctx, apiConfig.PostgresURL, postgres.DefaultConfig(),
	)
	if err != nil {
		panic(err)
	}
	defer postgresqlClient.Close()

	dbMetrics, err := dbmetrics.NewDBMetrics(dbSubsystem, postgresqlClient, "api")
	if err != nil {
		panic(err)
	}

	rabbitmqClient, err := rabbitmq.NewClient(log, apiConfig.RabbitMQURL, rabbitmq.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer rabbitmqClient.Close()

	userRepository := repository.NewUserRepository(log, postgresqlClient)
	userService := users.NewUserService(log, userRepository)
	userHandler := users.NewUserHandler(log, userService)

	sessionRepository := repository.NewSessionRepository(log, postgresqlClient)
	sessionService := sessions.NewSessionService(log, sessionRepository)
	authMiddleware := middleware.Auth(log, sessionService)
	sessionHandler := sessions.NewSessionHandler(log, sessionService, userService)

	instrumentRepository := repository.NewInstrumentRepository(log, postgresqlClient)
	instrumentService := instruments.NewInstrumentService(log, instrumentRepository)
	instrumentHandler := instruments.NewInstrumentHandler(log, instrumentService)

	marketRepository := repository.NewMarketRepository(log, postgresqlClient)
	marketService := markets.NewMarketService(log, marketRepository)
	marketHandler := markets.NewMarketHandler(log, marketService)

	const cacheRefreshSeconds = 5 * 60
	cacheService := cache.NewCacheService(log, marketRepository, instrumentRepository, cacheRefreshSeconds)
	if err = cacheService.Start(ctx); err != nil {
		panic(err)
	}
	defer cacheService.Stop()

	activeMarkets := cacheService.GetMarkets()
	marketRefs := make([]string, len(activeMarkets))
	marketQuanta := make(map[string]uint64, len(activeMarkets))
	for i, m := range activeMarkets {
		ref := utils.MergeMarketRef(m.BaseSymbol, m.QuoteSymbol)
		marketRefs[i] = ref
		marketQuanta[ref] = m.PriceQuantum
	}
	publisher := orderqueue.NewOrderCommandPublisher(log, rabbitmqClient, marketRefs, apiMetrics)

	orderRepository := repository.NewOrderRepository(log, postgresqlClient, dbMetrics)
	orderService := orders.NewOrderService(log, orderRepository, cacheService, publisher)
	orderHandler := orders.NewOrderHandler(log, orderService)

	streamHub, err := stream.NewHub(rabbitmqClient, marketRefs, log, apiMetrics)
	if err != nil {
		panic(err)
	}
	go streamHub.Run(ctx)
	streamHandler := stream.NewMarketsStreamHandler(log, streamHub, marketQuanta)

	srv := server.NewServer(server.ServerDependencies{
		Logger:             log,
		AuthMiddleware:     authMiddleware,
		Metrics:            apiMetrics,
		SessionsHandler:    sessionHandler,
		UsersHandler:       userHandler,
		InstrumentsHandler: instrumentHandler,
		MarketsHandler:     marketHandler,
		OrdersHandler:      orderHandler,
		StreamHandler:      streamHandler,
	})

	serverErrCh := make(chan error, 1)
	go func() {
		log.Info(fmt.Sprintf("starting server on %s:%d", apiConfig.ServerHost, apiConfig.ServerPort))
		serverErrCh <- srv.Start(apiConfig.ServerPort, apiConfig.ServerHost)
	}()

	quit, onQuit := os.OnSigIntAndTerm()
	defer onQuit()
	select {
	case sig := <-quit:
		log.Info(fmt.Sprintf("shutdown server ... signal=%s", sig))
	case err := <-serverErrCh:
		log.Error(fmt.Sprintf("server error: %v", err))
		return
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	log.Info("Shutting down API server...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(fmt.Sprintf("error shutting down server: %v", err))
	}
}
