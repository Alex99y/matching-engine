package markets

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/api/internal/stream"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

const price24hWindow = 24 * time.Hour

var (
	ErrMarketAlreadyExists = repository.ErrMarketAlreadyExists
	ErrMarketNotFound      = repository.ErrMarketNotFound
	ErrCreatingMarket      = errors.New("error creating market")
	ErrGettingMarket       = repository.ErrMarketGetFailed
	ErrDeletingMarket      = errors.New("error deleting market")
	ErrInvalidMarketRef    = utils.ErrInvalidMarketRef
	ErrInvalidInstruments  = repository.ErrInvalidInstruments
)

type Market struct {
	BaseSymbol    string
	QuoteSymbol   string
	PriceQuantum  uint64
	AmountQuantum uint64
	MinOrderSize  uint64
	MaxOrderSize  uint64
	TakerFeeBps   uint64
	MakerFeeBps   uint64
}

type MarketPrice struct {
	BaseSymbol   string
	QuoteSymbol  string
	Price        *uint64
	MinPrice24h  *uint64
	MaxPrice24h  *uint64
	Volume24h    *uint64
	OpenPrice24h *uint64
}

// DepthLevel is one price level of a depth snapshot.
type DepthLevel struct {
	Price    uint64
	Quantity uint64
}

// MarketDepth is a one-shot snapshot of a market's order book.
type MarketDepth struct {
	Market string
	Bids   []DepthLevel
	Asks   []DepthLevel
}

type MarketRepository interface {
	CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64, takerFeeBps, makerFeeBps int64) error
	GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error)
	GetMarkets(ctx context.Context) ([]repository.Market, error)
	GetLatestPrices(ctx context.Context, windowStart time.Time) ([]repository.MarketPrice, error)
	RemoveOneMarket(ctx context.Context, baseSymbol, quoteSymbol string) error
}

// DepthSource serves the in-memory book snapshot backing GetDepth — no DB read, unlike
// MarketRepository. Implemented directly by *stream.Hub.
type DepthSource interface {
	Depth(ctx context.Context, market string, group uint64) (*stream.MarketDepth, bool, error)
}

type MarketService struct {
	logger           *logger.Logger
	marketRepository MarketRepository
	depthSource      DepthSource
}

func (s *MarketService) CreateMarket(
	ctx context.Context,
	marketRef string,
	priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64,
) error {
	baseSymbol, quoteSymbol, err := utils.SplitMarketRef(marketRef)
	if err != nil {
		return err
	}

	// The public HTTP API does not expose fees yet; markets are created with zero fees.
	// Set fees via the CLI (or a future API field).
	if err := s.marketRepository.CreateMarket(ctx, baseSymbol, quoteSymbol, priceQuantum, amountQuantum, minOrderSize, maxOrderSize, 0, 0); err != nil {
		if errors.Is(err, repository.ErrMarketAlreadyExists) {
			return ErrMarketAlreadyExists
		}
		if errors.Is(err, repository.ErrInvalidInstruments) {
			return ErrInvalidInstruments
		}
		return ErrCreatingMarket
	}
	return nil
}

func (s *MarketService) GetMarket(ctx context.Context, marketRef string) (*Market, error) {
	baseSymbol, quoteSymbol, err := utils.SplitMarketRef(marketRef)
	if err != nil {
		return nil, err
	}

	m, err := s.marketRepository.GetMarket(ctx, baseSymbol, quoteSymbol)
	if err != nil {
		if errors.Is(err, repository.ErrMarketNotFound) {
			return nil, ErrMarketNotFound
		}
		return nil, ErrGettingMarket
	}
	return &Market{
		BaseSymbol:    m.BaseSymbol,
		QuoteSymbol:   m.QuoteSymbol,
		PriceQuantum:  m.PriceQuantum,
		AmountQuantum: m.AmountQuantum,
		MinOrderSize:  m.MinOrderSize,
		MaxOrderSize:  m.MaxOrderSize,
		TakerFeeBps:   m.TakerFeeBps,
		MakerFeeBps:   m.MakerFeeBps,
	}, nil
}

func (s *MarketService) GetMarkets(ctx context.Context) ([]Market, error) {
	repoMarkets, err := s.marketRepository.GetMarkets(ctx)
	if err != nil {
		return nil, ErrGettingMarket
	}
	markets := make([]Market, len(repoMarkets))
	for i, m := range repoMarkets {
		markets[i] = Market{
			BaseSymbol:    m.BaseSymbol,
			QuoteSymbol:   m.QuoteSymbol,
			PriceQuantum:  m.PriceQuantum,
			AmountQuantum: m.AmountQuantum,
			MinOrderSize:  m.MinOrderSize,
			MaxOrderSize:  m.MaxOrderSize,
			TakerFeeBps:   m.TakerFeeBps,
			MakerFeeBps:   m.MakerFeeBps,
		}
	}
	return markets, nil
}

func (s *MarketService) GetPrices(ctx context.Context) ([]MarketPrice, error) {
	windowStart := time.Now().Add(-price24hWindow)
	repoPrices, err := s.marketRepository.GetLatestPrices(ctx, windowStart)
	if err != nil {
		return nil, ErrGettingMarket
	}
	prices := make([]MarketPrice, len(repoPrices))
	for i, p := range repoPrices {
		prices[i] = MarketPrice{
			BaseSymbol:   p.BaseSymbol,
			QuoteSymbol:  p.QuoteSymbol,
			Price:        p.Price,
			MinPrice24h:  p.MinPrice24h,
			MaxPrice24h:  p.MaxPrice24h,
			Volume24h:    p.Volume24h,
			OpenPrice24h: p.OpenPrice24h,
		}
	}
	return prices, nil
}

// GetDepth returns a one-shot order-book snapshot for market, bucketed to group price units. It
// never touches the DB — market must already be a served market ref (validated by the handler
// against the same in-memory price-quantum map the depth source itself was built from).
func (s *MarketService) GetDepth(ctx context.Context, market string, group uint64) (*MarketDepth, error) {
	d, found, err := s.depthSource.Depth(ctx, market, group)
	if err != nil {
		return nil, ErrGettingMarket
	}
	if !found {
		return nil, ErrMarketNotFound
	}
	return &MarketDepth{
		Market: d.Market,
		Bids:   toDepthLevels(d.Bids),
		Asks:   toDepthLevels(d.Asks),
	}, nil
}

func toDepthLevels(levels []stream.DepthLevel) []DepthLevel {
	out := make([]DepthLevel, len(levels))
	for i, l := range levels {
		out[i] = DepthLevel{Price: l.Price, Quantity: l.Quantity}
	}
	return out
}

func (s *MarketService) RemoveOneMarket(ctx context.Context, marketRef string) error {
	baseSymbol, quoteSymbol, err := utils.SplitMarketRef(marketRef)
	if err != nil {
		return err
	}

	if err := s.marketRepository.RemoveOneMarket(ctx, baseSymbol, quoteSymbol); err != nil {
		if errors.Is(err, repository.ErrMarketNotFound) {
			return ErrMarketNotFound
		}
		return ErrDeletingMarket
	}
	return nil
}

func NewMarketService(
	logger *logger.Logger,
	marketRepository MarketRepository,
	depthSource DepthSource,
) *MarketService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if marketRepository == nil {
		panic("market repository cannot be nil")
	}
	if depthSource == nil {
		panic("depth source cannot be nil")
	}
	return &MarketService{
		logger:           logger,
		marketRepository: marketRepository,
		depthSource:      depthSource,
	}
}
