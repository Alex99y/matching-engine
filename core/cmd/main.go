package main

import (
	"context"
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	cmos "github.com/alex99y/matching-engine/common/pkg/os"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/core/internal/config"
	"github.com/alex99y/matching-engine/core/internal/orderprocessors"
	"github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/cache"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func main() {
	coreConfig := config.NewCoreConfig()
	log := logger.NewLogger(coreConfig.DebugLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	postgresqlClient, err := postgres.Connect(ctx, coreConfig.PostgresURL, postgres.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer postgresqlClient.Close()

	rabbitmqClient, err := rabbitmq.NewClient(log, coreConfig.RabbitMQURL, rabbitmq.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer rabbitmqClient.Close()

	instrumentRepository := repository.NewInstrumentRepository(log, postgresqlClient)
	marketRepository := repository.NewMarketRepository(log, postgresqlClient)
	orderRepository := repository.NewOrderRepository(log, postgresqlClient)

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
		p := orderprocessors.NewOrderProcessor(log, market, queue, orderRepository)
		go p.Start(ctx)
	}

	quit, onQuit := cmos.OnSigIntAndTerm()
	defer onQuit()

	sig := <-quit
	log.Info(fmt.Sprintf("shutting down ... signal=%s", sig))
	cancel()
}
