package main

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/observability"
	cmos "github.com/alex99y/matching-engine/common/pkg/os"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/core/internal/config"
	coremetrics "github.com/alex99y/matching-engine/core/internal/metrics"
	"github.com/alex99y/matching-engine/core/internal/orderprocessors"
	"github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/cache"
	dbmetrics "github.com/alex99y/matching-engine/db/pkg/metrics"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func main() {
	coreConfig := config.NewCoreConfig()
	log := logger.NewLogger(coreConfig.DebugLevel)

	// Observability: dedicated registry (Go runtime + process collectors preloaded) and a single
	// /metrics server on MetricsPort. coreMetrics (me_core_*) is populated by the order processors
	// and book; me_db_* is added with the repo over the same registry. core has no HTTP server of
	// its own, so this is the only listener besides the AMQP consumers.
	metricsRegistry := observability.NewServiceRegistry()
	coreSubsystem := observability.NewSubsystemMetrics(metricsRegistry, "me", "core")
	dbSubsystem := observability.NewSubsystemMetrics(metricsRegistry, "me", "db")
	coreMetrics, err := coremetrics.NewCoreMetrics(coreSubsystem)
	if err != nil {
		panic(err)
	}
	metricsServer := observability.NewPrometheusServer(coreConfig.MetricsPort, coreSubsystem, log)
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

	postgresqlClient, err := postgres.Connect(ctx, coreConfig.PostgresURL, postgres.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer postgresqlClient.Close()

	// me_db_* metrics: query latency/errors recorder + a scrape-time pool collector for this
	// process's pool, labeled service="core".
	dbMetrics, err := dbmetrics.NewDBMetrics(dbSubsystem, postgresqlClient, "core")
	if err != nil {
		panic(err)
	}

	rabbitmqClient, err := rabbitmq.NewClient(log, coreConfig.RabbitMQURL, rabbitmq.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer rabbitmqClient.Close()

	instrumentRepository := repository.NewInstrumentRepository(log, postgresqlClient)
	marketRepository := repository.NewMarketRepository(log, postgresqlClient)
	orderRepository := repository.NewOrderRepository(log, postgresqlClient, dbMetrics)

	const cacheRefreshSeconds = 5 * 60
	cacheService := cache.NewCacheService(log, marketRepository, instrumentRepository, cacheRefreshSeconds)
	if err = cacheService.Start(ctx); err != nil {
		panic(err)
	}
	defer cacheService.Stop()

	marketsToProcess := make([]*repository.Market, 0)
	for _, market := range coreConfig.MarketList {
		base, quote, err := utils.SplitMarketRef(market)
		if err != nil {
			panic(err)
		}
		marketInfo, err := cacheService.GetMarket(base, quote)
		if err != nil {
			panic(err)
		}
		marketsToProcess = append(marketsToProcess, marketInfo)
	}

	for _, market := range marketsToProcess {
		marketRef := utils.MergeMarketRef(market.BaseSymbol, market.QuoteSymbol)
		queue := order_events_queue.NewOrdersQueue(log, marketRef, rabbitmqClient)
		p := orderprocessors.NewOrderProcessor(log, market, queue, orderRepository, coreMetrics)
		go p.Start(ctx)
	}

	quit, onQuit := cmos.OnSigIntAndTerm()
	defer onQuit()

	sig := <-quit
	log.Info(fmt.Sprintf("shutting down ... signal=%s", sig))
	cancel()
}
