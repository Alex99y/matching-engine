package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/api/internal/config"
	"github.com/alex99y/matching-engine/api/internal/instruments"
	"github.com/alex99y/matching-engine/api/internal/markets"
	"github.com/alex99y/matching-engine/api/internal/orders"
	"github.com/alex99y/matching-engine/api/internal/server"
	"github.com/alex99y/matching-engine/api/internal/users"
	"github.com/alex99y/matching-engine/api/pkg/jwt"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/orderqueue"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/os"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/cache"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func main() {
	apiConfig := config.NewApiConfig()
	log := logger.NewLogger(apiConfig.DebugLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Instantiate clients

	postgresqlClient, err := postgres.Connect(
		ctx, apiConfig.PostgresURL, postgres.DefaultConfig(),
	)

	if err != nil {
		panic(err)
	}

	defer postgresqlClient.Close()

	rabbitmqClient, err := rabbitmq.NewClient(log, apiConfig.RabbitMQURL, rabbitmq.DefaultConfig())

	if err != nil {
		panic(err)
	}

	defer rabbitmqClient.Close()

	// Instantiate services and handlers
	jwtManager := jwt.NewJWTManager(apiConfig.JWTSecret)
	authMiddleware := middleware.Auth(jwtManager)

	userRepository := repository.NewUserRepository(log, postgresqlClient)
	userService := users.NewUserService(log, jwtManager, userRepository)
	userHandler := users.NewUserHandler(log, userService)

	instrumentRepository := repository.NewInstrumentRepository(log, postgresqlClient)
	instrumentService := instruments.NewInstrumentService(log, instrumentRepository)
	instrumentHandler := instruments.NewInstrumentHandler(log, instrumentService)

	marketRepository := repository.NewMarketRepository(log, postgresqlClient)
	marketService := markets.NewMarketService(log, marketRepository)
	marketHandler := markets.NewMarketHandler(log, marketService)

	const cacheRefreshSeconds = 5 * 60 // or better: read from apiConfig
	cacheService := cache.NewCacheService(log, marketRepository, instrumentRepository, cacheRefreshSeconds)
	if err = cacheService.Start(ctx); err != nil {
		panic(err)
	}
	defer cacheService.Stop()

	activeMarkets := cacheService.GetMarkets()
	marketRefs := make([]string, len(activeMarkets))
	for i, m := range activeMarkets {
		marketRefs[i] = utils.MergeMarketRef(m.BaseSymbol, m.QuoteSymbol)
	}
	publisher := orderqueue.NewOrderCommandPublisher(log, rabbitmqClient, marketRefs)

	orderRepository := repository.NewOrderRepository(log, postgresqlClient)
	orderService := orders.NewOrderService(log, orderRepository, cacheService, publisher)
	orderHandler := orders.NewOrderHandler(log, orderService)

	server := server.NewServer(server.ServerDependencies{
		Logger:             log,
		AuthMiddleware:     authMiddleware,
		UsersHandler:       userHandler,
		InstrumentsHandler: instrumentHandler,
		MarketsHandler:     marketHandler,
		OrdersHandler:      orderHandler,
	})

	serverErrCh := make(chan error, 1)
	go func() {
		log.Info(
			fmt.Sprintf(
				"starting server on %s:%d", apiConfig.ServerHost, apiConfig.ServerPort,
			),
		)
		serverErrCh <- server.Start(apiConfig.ServerPort, apiConfig.ServerHost)
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

	// Independent deadline — gives the HTTP server time to drain
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	log.Info("Shutting down API server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error(fmt.Sprintf("error shutting down server: %v", err))
	}
}
