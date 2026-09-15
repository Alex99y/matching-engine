package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var errRepo = errors.New("repository unavailable")

type fakeMarketRepo struct {
	mu      sync.Mutex
	markets []repository.Market
	err     error
	calls   atomic.Int64
}

func (f *fakeMarketRepo) GetMarkets(ctx context.Context) ([]repository.Market, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]repository.Market(nil), f.markets...), nil
}

func (f *fakeMarketRepo) set(markets []repository.Market, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markets, f.err = markets, err
}

type fakeInstrumentRepo struct {
	mu          sync.Mutex
	instruments []repository.Instrument
	err         error
}

func (f *fakeInstrumentRepo) GetInstruments(ctx context.Context) ([]repository.Instrument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]repository.Instrument(nil), f.instruments...), nil
}

func (f *fakeInstrumentRepo) set(instruments []repository.Instrument, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instruments, f.err = instruments, err
}

func seedMarkets() []repository.Market {
	return []repository.Market{
		{ID: 1, BaseSymbol: "BTC", QuoteSymbol: "USDT", PriceQuantum: 100, BaseScale: 1_000_000_000},
		{ID: 2, BaseSymbol: "ETH", QuoteSymbol: "USDT", PriceQuantum: 10, BaseScale: 1_000_000_000},
	}
}

func seedInstruments() []repository.Instrument {
	return []repository.Instrument{
		{ID: 10, Symbol: "BTC", Name: "Bitcoin", Decimals: 9},
		{ID: 11, Symbol: "USDT", Name: "Tether", Decimals: 6},
	}
}

func newTestCache(t *testing.T, ttlSeconds uint) (*CacheService, *fakeMarketRepo, *fakeInstrumentRepo) {
	t.Helper()
	markets := &fakeMarketRepo{markets: seedMarkets()}
	instruments := &fakeInstrumentRepo{instruments: seedInstruments()}
	return NewCacheService(logger.NewLogger(logger.Error), markets, instruments, ttlSeconds), markets, instruments
}

func startedCache(t *testing.T) (*CacheService, *fakeMarketRepo, *fakeInstrumentRepo) {
	t.Helper()
	c, markets, instruments := newTestCache(t, 3600)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(c.Stop)
	return c, markets, instruments
}

// Every lookup key is built by refresh, and core routes a cancel to a queue by asking for a market
// by id. A market that loads but is missing from one of the three indexes is reachable one way and
// invisible the other, so all of them are checked against the same seed.
func TestRefreshBuildsEveryIndex(t *testing.T) {
	c, _, _ := startedCache(t)

	byRef, err := c.GetMarketByRef("BTC-USDT")
	if err != nil {
		t.Fatalf("GetMarketByRef: %v", err)
	}
	bySymbols, err := c.GetMarket("BTC", "USDT")
	if err != nil {
		t.Fatalf("GetMarket: %v", err)
	}
	byID, err := c.GetMarketByID(1)
	if err != nil {
		t.Fatalf("GetMarketByID: %v", err)
	}

	if byRef.ID != 1 || bySymbols.ID != 1 || byID.ID != 1 {
		t.Fatalf("indexes disagree: byRef=%d bySymbols=%d byID=%d", byRef.ID, bySymbols.ID, byID.ID)
	}
	if byID.BaseSymbol != "BTC" || byID.QuoteSymbol != "USDT" {
		t.Fatalf("market by id = %s-%s, want BTC-USDT", byID.BaseSymbol, byID.QuoteSymbol)
	}

	instBySymbol, err := c.GetInstrument("BTC")
	if err != nil {
		t.Fatalf("GetInstrument: %v", err)
	}
	instByID, err := c.GetInstrumentByID(10)
	if err != nil {
		t.Fatalf("GetInstrumentByID: %v", err)
	}
	if instBySymbol.ID != 10 || instByID.Symbol != "BTC" {
		t.Fatalf("instrument indexes disagree: %+v %+v", instBySymbol, instByID)
	}

	if len(c.GetMarkets()) != 2 || len(c.GetInstruments()) != 2 {
		t.Fatalf("listings = %d markets, %d instruments; want 2 and 2",
			len(c.GetMarkets()), len(c.GetInstruments()))
	}
}

// Every index has to distinguish "no such entry" from a zero value, or a caller would act on an
// empty market: core would publish a cancel to the ref "-" and the API would price against a
// BaseScale of 0.
func TestMissingEntriesReturnSentinelErrors(t *testing.T) {
	c, _, _ := startedCache(t)

	marketLookups := map[string]func() error{
		"by ref":     func() error { _, err := c.GetMarketByRef("DOGE-USDT"); return err },
		"by symbols": func() error { _, err := c.GetMarket("DOGE", "USDT"); return err },
		"by id":      func() error { _, err := c.GetMarketByID(999); return err },
	}
	for name, lookup := range marketLookups {
		t.Run("market "+name, func(t *testing.T) {
			if err := lookup(); !errors.Is(err, ErrMarketNotFound) {
				t.Fatalf("err = %v, want ErrMarketNotFound", err)
			}
		})
	}

	instrumentLookups := map[string]func() error{
		"by symbol": func() error { _, err := c.GetInstrument("DOGE"); return err },
		"by id":     func() error { _, err := c.GetInstrumentByID(999); return err },
	}
	for name, lookup := range instrumentLookups {
		t.Run("instrument "+name, func(t *testing.T) {
			if err := lookup(); !errors.Is(err, ErrInstrumentNotFound) {
				t.Fatalf("err = %v, want ErrInstrumentNotFound", err)
			}
		})
	}
}

// The market key is built with the same MergeMarketRef the rest of the engine uses, so a ref
// assembled anywhere must find the market the symbols find.
func TestMarketRefKeyMatchesTheSymbolPair(t *testing.T) {
	c, _, _ := startedCache(t)

	bySymbols, err := c.GetMarket("ETH", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	byRef, err := c.GetMarketByRef("ETH-USDT")
	if err != nil {
		t.Fatal(err)
	}
	if bySymbols != byRef {
		t.Fatal("symbol pair and market ref resolved to different markets")
	}

	// Reversing the pair is a different market, not the same one.
	if _, err := c.GetMarket("USDT", "ETH"); !errors.Is(err, ErrMarketNotFound) {
		t.Fatalf("reversed pair resolved: %v", err)
	}
}

// Start loads synchronously so that a caller that gets a nil error can rely on the cache being
// populated. Serving an empty cache because the first load failed would look like "no markets
// configured" rather than "the database is down".
func TestStartFailsWhenTheFirstLoadFails(t *testing.T) {
	for _, tt := range []struct {
		name string
		fail func(*fakeMarketRepo, *fakeInstrumentRepo)
	}{
		{"markets unavailable", func(m *fakeMarketRepo, _ *fakeInstrumentRepo) { m.set(nil, errRepo) }},
		{"instruments unavailable", func(_ *fakeMarketRepo, i *fakeInstrumentRepo) { i.set(nil, errRepo) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, markets, instruments := newTestCache(t, 3600)
			tt.fail(markets, instruments)

			err := c.Start(context.Background())
			if !errors.Is(err, ErrCacheRefreshFailed) {
				t.Fatalf("err = %v, want ErrCacheRefreshFailed", err)
			}
			if len(c.GetMarkets()) != 0 {
				t.Fatal("a failed start must not populate the cache")
			}
		})
	}
}

// startOnce means a second Start is a silent no-op that returns nil, so a caller must not read that
// nil as "started". The failed first Start also leaves no goroutine behind for Stop to wait on.
func TestStartIsOnlyEverEffectiveOnce(t *testing.T) {
	c, markets, _ := newTestCache(t, 3600)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	t.Cleanup(c.Stop)
	after := markets.calls.Load()

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if markets.calls.Load() != after {
		t.Fatalf("second Start refreshed again: %d calls, want %d", markets.calls.Load(), after)
	}
}

func TestStopBeforeStartDoesNotPanic(t *testing.T) {
	c, _, _ := newTestCache(t, 3600)
	c.Stop()
}

// The background refresh is what picks up a market added while the engine is running.
func TestBackgroundRefreshPicksUpNewMarkets(t *testing.T) {
	c, markets, _ := newTestCache(t, 1)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.Stop()

	if _, err := c.GetMarketByRef("SOL-USDT"); !errors.Is(err, ErrMarketNotFound) {
		t.Fatal("market present before it was added")
	}

	markets.set(append(seedMarkets(), repository.Market{
		ID: 3, BaseSymbol: "SOL", QuoteSymbol: "USDT",
	}), nil)

	deadline := time.After(5 * time.Second)
	for {
		if _, err := c.GetMarketByRef("SOL-USDT"); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("background refresh never picked up the new market")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// A refresh that fails mid-life must leave the previous snapshot intact. Swapping in an empty one
// would make every market vanish from a running engine because the database blipped.
func TestAFailedRefreshServesTheLastGoodSnapshot(t *testing.T) {
	c, markets, _ := startedCache(t)

	markets.set(nil, errRepo)
	if err := c.refresh(context.Background()); !errors.Is(err, ErrCacheRefreshFailed) {
		t.Fatalf("err = %v, want ErrCacheRefreshFailed", err)
	}

	if _, err := c.GetMarketByRef("BTC-USDT"); err != nil {
		t.Fatalf("a failed refresh dropped the cached markets: %v", err)
	}
	if len(c.GetMarkets()) != 2 {
		t.Fatalf("markets = %d, want the 2 from the last good refresh", len(c.GetMarkets()))
	}
}

// Readers hold only an RLock, so a refresh landing concurrently must not tear the snapshot or race.
// Worth asserting explicitly because the pointers handed out by the getters point into the slice
// that refresh replaces wholesale. Run with -race to mean anything.
func TestConcurrentReadsDuringRefresh(t *testing.T) {
	c, _, _ := startedCache(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if m, err := c.GetMarketByID(1); err == nil && m.BaseSymbol != "BTC" {
						t.Errorf("torn read: %+v", m)
						return
					}
					c.GetMarkets()
					c.GetInstruments()
				}
			}
		}()
	}

	for range 50 {
		if err := c.refresh(context.Background()); err != nil {
			t.Errorf("refresh: %v", err)
			break
		}
	}
	close(stop)
	wg.Wait()
}

// The constructor fails fast on a dependency it cannot work without, so a misconfigured process
// dies at startup rather than serving an empty cache.
func TestNewCacheServiceRejectsMissingDependencies(t *testing.T) {
	log := logger.NewLogger(logger.Error)
	markets := &fakeMarketRepo{}
	instruments := &fakeInstrumentRepo{}

	tests := []struct {
		name string
		call func()
	}{
		{"nil logger", func() { NewCacheService(nil, markets, instruments, 60) }},
		{"nil market repository", func() { NewCacheService(log, nil, instruments, 60) }},
		{"nil instrument repository", func() { NewCacheService(log, markets, nil, 60) }},
		{"zero ttl", func() { NewCacheService(log, markets, instruments, 0) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			tt.call()
		})
	}
}
