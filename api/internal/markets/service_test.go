package markets_test

import (
	"context"
	"errors"
	"strings"
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

// spyMarketRepository records CreateMarket/GetMarket/GetMarkets/RemoveOneMarket calls and lets
// each return a canned result — stubMarketRepository above only varies GetLatestPrices.
type spyMarketRepository struct {
	stubMarketRepository

	createCalls []createMarketCall
	createErr   error

	market    *repository.Market
	marketErr error

	markets    []repository.Market
	marketsErr error

	removeCalls []removeMarketCall
	removeErr   error
}

type createMarketCall struct {
	baseSymbol, quoteSymbol                                 string
	priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64
	takerFeeBps, makerFeeBps                                int64
}

type removeMarketCall struct {
	baseSymbol, quoteSymbol string
}

func (s *spyMarketRepository) CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64, takerFeeBps, makerFeeBps int64) error {
	s.createCalls = append(s.createCalls, createMarketCall{baseSymbol, quoteSymbol, priceQuantum, amountQuantum, minOrderSize, maxOrderSize, takerFeeBps, makerFeeBps})
	return s.createErr
}

func (s *spyMarketRepository) GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error) {
	return s.market, s.marketErr
}

func (s *spyMarketRepository) GetMarkets(ctx context.Context) ([]repository.Market, error) {
	return s.markets, s.marketsErr
}

func (s *spyMarketRepository) RemoveOneMarket(ctx context.Context, baseSymbol, quoteSymbol string) error {
	s.removeCalls = append(s.removeCalls, removeMarketCall{baseSymbol, quoteSymbol})
	return s.removeErr
}

func TestCreateMarketSuccessSplitsRefAndZeroesFees(t *testing.T) {
	repo := &spyMarketRepository{}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	if err := svc.CreateMarket(context.Background(), "BTC-USDT", 1, 1000, 1000, 1000000000); err != nil {
		t.Fatalf("CreateMarket: %v", err)
	}
	if len(repo.createCalls) != 1 {
		t.Fatalf("createCalls = %d, want 1", len(repo.createCalls))
	}
	got := repo.createCalls[0]
	// The public API doesn't expose fees yet — CreateMarket must always pass 0, 0.
	want := createMarketCall{"BTC", "USDT", 1, 1000, 1000, 1000000000, 0, 0}
	if got != want {
		t.Fatalf("createCalls[0] = %+v, want %+v", got, want)
	}
}

func TestCreateMarketInvalidRef(t *testing.T) {
	svc := newTestServiceWithRepo(&spyMarketRepository{}, fakeDepthSource{})

	err := svc.CreateMarket(context.Background(), "NOTAMARKET", 1, 1, 1, 1)
	if !errors.Is(err, markets.ErrInvalidMarketRef) {
		t.Fatalf("err = %v, want ErrInvalidMarketRef", err)
	}
}

func TestCreateMarketAlreadyExists(t *testing.T) {
	repo := &spyMarketRepository{createErr: repository.ErrMarketAlreadyExists}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	err := svc.CreateMarket(context.Background(), "BTC-USDT", 1, 1, 1, 1)
	if !errors.Is(err, markets.ErrMarketAlreadyExists) {
		t.Fatalf("err = %v, want ErrMarketAlreadyExists", err)
	}
}

func TestCreateMarketInvalidInstruments(t *testing.T) {
	repo := &spyMarketRepository{createErr: repository.ErrInvalidInstruments}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	err := svc.CreateMarket(context.Background(), "BTC-USDT", 1, 1, 1, 1)
	if !errors.Is(err, markets.ErrInvalidInstruments) {
		t.Fatalf("err = %v, want ErrInvalidInstruments", err)
	}
}

func TestCreateMarketRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &spyMarketRepository{createErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	err := svc.CreateMarket(context.Background(), "BTC-USDT", 1, 1, 1, 1)
	if !errors.Is(err, markets.ErrCreatingMarket) {
		t.Fatalf("err = %v, want ErrCreatingMarket", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetMarketSuccess(t *testing.T) {
	repo := &spyMarketRepository{market: &repository.Market{
		BaseSymbol: "BTC", QuoteSymbol: "USDT", PriceQuantum: 1, AmountQuantum: 1000, MinOrderSize: 1000, MaxOrderSize: 1000000000,
	}}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	got, err := svc.GetMarket(context.Background(), "BTC-USDT")
	if err != nil {
		t.Fatalf("GetMarket: %v", err)
	}
	if got.BaseSymbol != "BTC" || got.QuoteSymbol != "USDT" || got.PriceQuantum != 1 {
		t.Fatalf("got = %+v, unexpected mapping", got)
	}
}

func TestGetMarketInvalidRef(t *testing.T) {
	svc := newTestServiceWithRepo(&spyMarketRepository{}, fakeDepthSource{})

	_, err := svc.GetMarket(context.Background(), "NOTAMARKET")
	if !errors.Is(err, markets.ErrInvalidMarketRef) {
		t.Fatalf("err = %v, want ErrInvalidMarketRef", err)
	}
}

func TestGetMarketNotFound(t *testing.T) {
	repo := &spyMarketRepository{marketErr: repository.ErrMarketNotFound}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	_, err := svc.GetMarket(context.Background(), "BTC-USDT")
	if !errors.Is(err, markets.ErrMarketNotFound) {
		t.Fatalf("err = %v, want ErrMarketNotFound", err)
	}
}

func TestGetMarketRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &spyMarketRepository{marketErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	_, err := svc.GetMarket(context.Background(), "BTC-USDT")
	if !errors.Is(err, markets.ErrGettingMarket) {
		t.Fatalf("err = %v, want ErrGettingMarket", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetMarketsSuccess(t *testing.T) {
	repo := &spyMarketRepository{markets: []repository.Market{
		{BaseSymbol: "BTC", QuoteSymbol: "USDT"},
		{BaseSymbol: "ETH", QuoteSymbol: "USDT"},
	}}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	got, err := svc.GetMarkets(context.Background())
	if err != nil {
		t.Fatalf("GetMarkets: %v", err)
	}
	if len(got) != 2 || got[0].BaseSymbol != "BTC" || got[1].BaseSymbol != "ETH" {
		t.Fatalf("got = %+v, want [BTC ETH]", got)
	}
}

func TestGetMarketsRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &spyMarketRepository{marketsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	_, err := svc.GetMarkets(context.Background())
	if !errors.Is(err, markets.ErrGettingMarket) {
		t.Fatalf("err = %v, want ErrGettingMarket", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestRemoveOneMarketSuccess(t *testing.T) {
	repo := &spyMarketRepository{}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	if err := svc.RemoveOneMarket(context.Background(), "BTC-USDT"); err != nil {
		t.Fatalf("RemoveOneMarket: %v", err)
	}
	if len(repo.removeCalls) != 1 || repo.removeCalls[0] != (removeMarketCall{"BTC", "USDT"}) {
		t.Fatalf("removeCalls = %+v, want one call with (BTC, USDT)", repo.removeCalls)
	}
}

func TestRemoveOneMarketNotFound(t *testing.T) {
	repo := &spyMarketRepository{removeErr: repository.ErrMarketNotFound}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	err := svc.RemoveOneMarket(context.Background(), "BTC-USDT")
	if !errors.Is(err, markets.ErrMarketNotFound) {
		t.Fatalf("err = %v, want ErrMarketNotFound", err)
	}
}

func TestRemoveOneMarketRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &spyMarketRepository{removeErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestServiceWithRepo(repo, fakeDepthSource{})

	err := svc.RemoveOneMarket(context.Background(), "BTC-USDT")
	if !errors.Is(err, markets.ErrDeletingMarket) {
		t.Fatalf("err = %v, want ErrDeletingMarket", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}
