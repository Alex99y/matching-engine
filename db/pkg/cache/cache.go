package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

const cacheErrPrefix = "cache:"

var (
	ErrCacheRefreshFailed = errors.New("cache refresh failed")
	ErrMarketNotFound     = errors.New("market not found")
	ErrInstrumentNotFound = errors.New("instrument not found")
)

type MarketRepository interface {
	GetMarkets(ctx context.Context) ([]repository.Market, error)
}

type InstrumentRepository interface {
	GetInstruments(ctx context.Context) ([]repository.Instrument, error)
}

type CacheService struct {
	logger         *logger.Logger
	marketRepo     MarketRepository
	instrumentRepo InstrumentRepository
	ttl            time.Duration

	mu                  sync.RWMutex
	markets             []repository.Market
	marketsByKey        map[string]*repository.Market
	marketsByID         map[int]*repository.Market
	instruments         []repository.Instrument
	instrumentsBySymbol map[string]*repository.Instrument
	instrumentsByID     map[int]*repository.Instrument

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// refresh fetches fresh data from the DB and then swaps the cached state
// under a write lock. The DB calls happen outside the lock so readers are
// never blocked for the duration of the network round-trip.
func (c *CacheService) refresh(ctx context.Context) error {
	markets, err := c.marketRepo.GetMarkets(ctx)
	if err != nil {
		c.logger.Error("cache: failed to refresh markets")
		c.logger.ErrorO(err)
		return fmt.Errorf("%s %w", cacheErrPrefix, ErrCacheRefreshFailed)
	}

	instruments, err := c.instrumentRepo.GetInstruments(ctx)
	if err != nil {
		c.logger.Error("cache: failed to refresh instruments")
		c.logger.ErrorO(err)
		return fmt.Errorf("%s %w", cacheErrPrefix, ErrCacheRefreshFailed)
	}

	marketsByKey := make(map[string]*repository.Market, len(markets))
	marketsByID := make(map[int]*repository.Market, len(markets))
	for i := range markets {
		key := utils.MergeMarketRef(markets[i].BaseSymbol, markets[i].QuoteSymbol)
		marketsByKey[key] = &markets[i]
		marketsByID[markets[i].ID] = &markets[i]
	}

	instrumentsBySymbol := make(map[string]*repository.Instrument, len(instruments))
	instrumentsByID := make(map[int]*repository.Instrument, len(instruments))
	for i := range instruments {
		instrumentsBySymbol[instruments[i].Symbol] = &instruments[i]
		instrumentsByID[instruments[i].ID] = &instruments[i]
	}

	c.mu.Lock()
	c.markets = markets
	c.marketsByKey = marketsByKey
	c.marketsByID = marketsByID
	c.instruments = instruments
	c.instrumentsBySymbol = instrumentsBySymbol
	c.instrumentsByID = instrumentsByID
	c.mu.Unlock()

	return nil
}

func (c *CacheService) run(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = c.refresh(ctx) // error already logged inside refresh; stale data is served until next successful tick
		case <-ctx.Done():
			return
		}
	}
}

// Start performs the initial synchronous load from the DB and then launches
// the background goroutine that refreshes the cache every ttl duration.
// It must be called before any Get method.
func (c *CacheService) Start(ctx context.Context) error {
	if err := c.refresh(ctx); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.wg.Add(1)
	go c.run(runCtx)
	return nil
}

// Stop signals the background goroutine to exit and blocks until it does.
func (c *CacheService) Stop() {
	if c.cancel != nil {
		c.cancel()
		c.wg.Wait()
	}
}

func (c *CacheService) GetMarkets() []repository.Market {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.markets
}

func (c *CacheService) GetMarket(baseSymbol, quoteSymbol string) (*repository.Market, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.marketsByKey[utils.MergeMarketRef(baseSymbol, quoteSymbol)]
	if !ok {
		return nil, fmt.Errorf("%s %w", cacheErrPrefix, ErrMarketNotFound)
	}
	return m, nil
}

func (c *CacheService) GetMarketByRef(marketRef string) (*repository.Market, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.marketsByKey[marketRef]
	if !ok {
		return nil, fmt.Errorf("%s %w", cacheErrPrefix, ErrMarketNotFound)
	}
	return m, nil
}

func (c *CacheService) GetMarketByID(id int) (*repository.Market, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.marketsByID[id]
	if !ok {
		return nil, fmt.Errorf("%s %w", cacheErrPrefix, ErrMarketNotFound)
	}
	return m, nil
}

func (c *CacheService) GetInstruments() []repository.Instrument {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.instruments
}

func (c *CacheService) GetInstrument(symbol string) (*repository.Instrument, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i, ok := c.instrumentsBySymbol[symbol]
	if !ok {
		return nil, fmt.Errorf("%s %w", cacheErrPrefix, ErrInstrumentNotFound)
	}
	return i, nil
}

func (c *CacheService) GetInstrumentByID(id int) (*repository.Instrument, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i, ok := c.instrumentsByID[id]
	if !ok {
		return nil, fmt.Errorf("%s %w", cacheErrPrefix, ErrInstrumentNotFound)
	}
	return i, nil
}

func NewCacheService(
	logger *logger.Logger,
	marketRepo MarketRepository,
	instrumentRepo InstrumentRepository,
	ttlSeconds uint,
) *CacheService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if marketRepo == nil {
		panic("market repository cannot be nil")
	}
	if instrumentRepo == nil {
		panic("instrument repository cannot be nil")
	}
	if ttlSeconds <= 0 {
		panic("ttl must be greater than 0")
	}
	return &CacheService{
		logger:         logger,
		marketRepo:     marketRepo,
		instrumentRepo: instrumentRepo,
		ttl:            time.Duration(ttlSeconds) * time.Second,
	}
}
