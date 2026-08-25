package markets_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/markets"
	"github.com/alex99y/matching-engine/api/internal/stream"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

// stubMarketRepository satisfies markets.MarketRepository. GetDepth never touches it, so every
// method other than GetLatestPrices is an unused stub.
type stubMarketRepository struct {
	prices    []repository.MarketPrice
	pricesErr error
}

func (stubMarketRepository) CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64, takerFeeBps, makerFeeBps int64) error {
	return nil
}

func (stubMarketRepository) GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error) {
	return nil, nil
}

func (stubMarketRepository) GetMarkets(ctx context.Context) ([]repository.Market, error) {
	return nil, nil
}

func (s stubMarketRepository) GetLatestPrices(ctx context.Context, windowStart time.Time) ([]repository.MarketPrice, error) {
	return s.prices, s.pricesErr
}

func (stubMarketRepository) RemoveOneMarket(ctx context.Context, baseSymbol, quoteSymbol string) error {
	return nil
}

type fakeDepthSource struct {
	depth *stream.MarketDepth
	found bool
	err   error
}

func (f fakeDepthSource) Depth(ctx context.Context, market string, group uint64) (*stream.MarketDepth, bool, error) {
	return f.depth, f.found, f.err
}

func newTestService(depthSource markets.DepthSource) *markets.MarketService {
	return newTestServiceWithRepo(stubMarketRepository{}, depthSource)
}

func newTestServiceWithRepo(repo markets.MarketRepository, depthSource markets.DepthSource) *markets.MarketService {
	return markets.NewMarketService(logger.NewLogger(logger.Error), repo, depthSource)
}

func TestGetDepthMapsFoundSnapshot(t *testing.T) {
	svc := newTestService(fakeDepthSource{
		found: true,
		depth: &stream.MarketDepth{
			Market: "BTC-USDT",
			Bids:   []stream.DepthLevel{{Price: 100, Quantity: 2}},
			Asks:   []stream.DepthLevel{{Price: 101, Quantity: 4}},
		},
	})

	depth, err := svc.GetDepth(context.Background(), "BTC-USDT", 1)
	if err != nil {
		t.Fatalf("GetDepth: %v", err)
	}
	if depth.Market != "BTC-USDT" || len(depth.Bids) != 1 || depth.Bids[0].Price != 100 || depth.Bids[0].Quantity != 2 {
		t.Fatalf("depth = %+v, want bids [{100 2}]", depth)
	}
	if len(depth.Asks) != 1 || depth.Asks[0].Price != 101 || depth.Asks[0].Quantity != 4 {
		t.Fatalf("depth = %+v, want asks [{101 4}]", depth)
	}
}

func TestGetDepthUnknownMarketReturnsErrMarketNotFound(t *testing.T) {
	svc := newTestService(fakeDepthSource{found: false})

	_, err := svc.GetDepth(context.Background(), "NOPE-NOPE", 1)
	if !errors.Is(err, markets.ErrMarketNotFound) {
		t.Fatalf("err = %v, want ErrMarketNotFound", err)
	}
}

func TestGetDepthSourceErrorReturnsErrGettingMarket(t *testing.T) {
	svc := newTestService(fakeDepthSource{err: errors.New("boom")})

	_, err := svc.GetDepth(context.Background(), "BTC-USDT", 1)
	if !errors.Is(err, markets.ErrGettingMarket) {
		t.Fatalf("err = %v, want ErrGettingMarket", err)
	}
}

func u64(v uint64) *uint64 { return &v }

func TestGetPricesMapsOpenPrice24h(t *testing.T) {
	repo := stubMarketRepository{prices: []repository.MarketPrice{
		{
			BaseSymbol: "BTC", QuoteSymbol: "USDT",
			Price: u64(110), MinPrice24h: u64(90), MaxPrice24h: u64(120),
			Volume24h: u64(5), OpenPrice24h: u64(100),
		},
		{
			// No trades in the last 24h: everything windowed stays nil, same as today.
			BaseSymbol: "ETH", QuoteSymbol: "USDT",
			Price: u64(2000),
		},
	}}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	got, err := svc.GetPrices(context.Background())
	if err != nil {
		t.Fatalf("GetPrices: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if *got[0].OpenPrice24h != 100 {
		t.Fatalf("OpenPrice24h = %d, want 100", *got[0].OpenPrice24h)
	}
	if got[1].OpenPrice24h != nil {
		t.Fatalf("OpenPrice24h = %v, want nil", got[1].OpenPrice24h)
	}
}

func TestGetPricesRepositoryErrorReturnsErrGettingMarket(t *testing.T) {
	repo := stubMarketRepository{pricesErr: errors.New("boom")}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	_, err := svc.GetPrices(context.Background())
	if !errors.Is(err, markets.ErrGettingMarket) {
		t.Fatalf("err = %v, want ErrGettingMarket", err)
	}
}
