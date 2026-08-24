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
// method here is an unused stub.
type stubMarketRepository struct{}

func (stubMarketRepository) CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64, takerFeeBps, makerFeeBps int64) error {
	return nil
}

func (stubMarketRepository) GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error) {
	return nil, nil
}

func (stubMarketRepository) GetMarkets(ctx context.Context) ([]repository.Market, error) {
	return nil, nil
}

func (stubMarketRepository) GetLatestPrices(ctx context.Context, windowStart time.Time) ([]repository.MarketPrice, error) {
	return nil, nil
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
	return markets.NewMarketService(logger.NewLogger(logger.Error), stubMarketRepository{}, depthSource)
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
